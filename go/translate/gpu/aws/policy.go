package aws

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
)

// Names name a deployment's AWS side after one word: its operator policy, the
// instance's role and profile, its security groups and the Project tag every
// instance and volume it launches carries.
type Names string

// Policy is the operator policy's name.
func (n Names) Policy() string { return string(n) + "-operator" }

// PolicyARN is the operator policy's ARN in an account.
func (n Names) PolicyARN(account string) string {
	return fmt.Sprintf("arn:%s:iam::%s:policy/%s", partition, account, n.Policy())
}

// InstanceProfile names the instance's role and its instance profile.
func (n Names) InstanceProfile() string { return string(n) + "-instance" }

// SecurityGroup is each region's security group's name.
func (n Names) SecurityGroup() string { return string(n) }

// Tag is the Project tag's value.
func (n Names) Tag() string { return string(n) }

// policy is an IAM policy document.
type policy struct {
	Version   string      `json:"Version"`
	Statement []statement `json:"Statement"`
}

// statement is one statement: Action, NotAction and Resource a string or a
// list.
type statement struct {
	Sid       string                    `json:"Sid"`
	Effect    string                    `json:"Effect"`
	Action    any                       `json:"Action,omitempty"`
	NotAction any                       `json:"NotAction,omitempty"`
	Resource  any                       `json:"Resource"`
	Condition map[string]map[string]any `json:"Condition,omitempty"`
}

const (
	// regionsSid names the statement confining the operator to the regions
	// setup prepared — what a run reads to know where it may rent.
	regionsSid = "OnlyInThePreparedRegions"
	// requestedRegion is the condition key naming a request's region.
	requestedRegion = "aws:RequestedRegion"
)

// operatorPolicy is what a deployment's operator may do in an account, in its
// regions: launch only the profiles' instance types, tagged with its Project
// tag and carrying its instance role; tunnel into, read the console of and
// terminate only instances so tagged; read where and at what price it may
// launch, and its own policy, whose regions are the ones it may rent in. One
// statement confines everything regional to those regions, so the policy's
// size does not grow with them past IAM's limit.
func operatorPolicy(account string, names Names, regions, types []string) policy {
	arn := func(service, account, resource string) string {
		return fmt.Sprintf("arn:%s:%s:*:%s:%s", partition, service, account, resource)
	}
	instances := arn("ec2", "*", "instance/*")
	volumes := arn("ec2", "*", "volume/*")
	tagged := func(key string) map[string]map[string]any {
		return map[string]map[string]any{"StringEquals": {key: names.Tag()}}
	}
	// What is not regional, or answers in the home region alone.
	unconfined := []string{"iam:GetPolicy", "iam:GetPolicyVersion", "iam:PassRole", "pricing:GetProducts"}
	statements := []statement{
		{Sid: regionsSid, Effect: "Deny", NotAction: unconfined, Resource: "*", Condition: map[string]map[string]any{"StringNotEquals": {requestedRegion: regions}}},
		{Sid: "LaunchOnlyTheTranslationInstance", Action: "ec2:RunInstances", Resource: instances, Condition: map[string]map[string]any{"StringEquals": {
			"ec2:InstanceType":       types,
			"aws:RequestTag/Project": names.Tag(),
		}}},
		{Sid: "WithTaggedVolumes", Action: "ec2:RunInstances", Resource: volumes, Condition: tagged("aws:RequestTag/Project")},
		{Sid: "FromAnyImageIntoAnySubnet", Action: "ec2:RunInstances",
			Resource: []string{arn("ec2", "", "image/*"), arn("ec2", "*", "subnet/*"), arn("ec2", "*", "network-interface/*"), arn("ec2", "*", "security-group/*")}},
		{Sid: "TagOnlyAtLaunch", Action: "ec2:CreateTags", Resource: []string{instances, volumes}, Condition: map[string]map[string]any{"StringEquals": {"ec2:CreateAction": "RunInstances"}}},
		{Sid: "EndAndDiagnoseOwnInstances", Action: []string{"ec2:TerminateInstances", "ec2:GetConsoleOutput"}, Resource: instances, Condition: tagged("aws:ResourceTag/Project")},
		{Sid: "PassTheInstanceRole", Action: "iam:PassRole", Resource: fmt.Sprintf("arn:%s:iam::%s:role/%s", partition, account, names.InstanceProfile()),
			Condition: map[string]map[string]any{"StringEquals": {"iam:PassedToService": "ec2.amazonaws.com"}}},
		{Sid: "ReadItsOwnPolicy", Action: []string{"iam:GetPolicy", "iam:GetPolicyVersion"}, Resource: names.PolicyARN(account)},
		{Sid: "Read", Action: []string{
			"ec2:DescribeInstances",
			"ec2:DescribeInstanceTypeOfferings",
			"ec2:DescribeInstanceTypes",
			"ec2:DescribeSecurityGroups",
			"ec2:DescribeSpotInstanceRequests",
			"ec2:DescribeSpotPriceHistory",
			"ec2:DescribeSubnets",
			"ssm:DescribeInstanceInformation",
			"servicequotas:GetServiceQuota",
			"pricing:GetProducts",
		}, Resource: "*"},
		{Sid: "ResolveTheDlami", Action: "ssm:GetParameter", Resource: arn("ssm", "", "parameter/aws/service/deeplearning/*")},
		{Sid: "TunnelIntoOwnInstances", Action: "ssm:StartSession", Resource: instances, Condition: tagged("ssm:resourceTag/Project")},
		{Sid: "AsAPortForward", Action: "ssm:StartSession", Resource: arn("ssm", "", "document/AWS-StartPortForwardingSession")},
		{Sid: "OwnSessions", Action: []string{"ssm:TerminateSession", "ssm:ResumeSession", "ssmmessages:OpenDataChannel"},
			Resource: []string{arn("ssm", "*", "session/${aws:username}-*"), arn("ssm", "*", "session/${aws:userid}-*")}},
	}
	for i := range statements {
		if statements[i].Effect == "" {
			statements[i].Effect = "Allow"
		}
	}
	return policy{Version: "2012-10-17", Statement: statements}
}

// document is the policy as IAM is given it.
func (p policy) document() string {
	data, err := json.Marshal(p)
	if err != nil {
		panic(fmt.Sprintf("gpu: marshal the operator policy: %v", err))
	}
	return string(data)
}

// decode parses a document as IAM returns it, URL-encoded, into out.
func decode(encoded string, out any) error {
	decoded, err := url.PathUnescape(encoded)
	if err != nil {
		return fmt.Errorf("decode the policy document: %w", err)
	}
	if err := json.Unmarshal([]byte(decoded), out); err != nil {
		return fmt.Errorf("parse the policy document: %w", err)
	}
	return nil
}

// sameDocument reports whether a document IAM returns says what the policy
// does.
func (p policy) sameDocument(encoded string) (bool, error) {
	var have, want any
	if err := decode(encoded, &have); err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(p.document()), &want); err != nil {
		return false, err
	}
	return reflect.DeepEqual(have, want), nil
}

// regionsIn are the regions a document IAM returns of an operator policy
// confines the operator to.
func regionsIn(encoded string) ([]string, error) {
	var p policy
	if err := decode(encoded, &p); err != nil {
		return nil, err
	}
	for _, s := range p.Statement {
		if s.Sid != regionsSid {
			continue
		}
		listed, _ := s.Condition["StringNotEquals"][requestedRegion].([]any)
		var regions []string
		for _, region := range listed {
			if name, ok := region.(string); ok {
				regions = append(regions, name)
			}
		}
		if len(regions) == 0 || len(regions) != len(listed) {
			return nil, fmt.Errorf("the operator policy's %s statement lists no regions", regionsSid)
		}
		return regions, nil
	}
	return nil, fmt.Errorf("the operator policy has no %s statement: run `translate-gpu setup` again", regionsSid)
}
