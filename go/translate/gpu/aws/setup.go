package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	sqtypes "github.com/aws/aws-sdk-go-v2/service/servicequotas/types"
)

// maxPolicyVersions is how many versions IAM keeps of a managed policy.
const maxPolicyVersions = 5

// Setup converges, as the account's administrator, what a run needs: the
// operator policy confined to the regions, admitting the profiles' instance
// types; the instance's role and profile; EC2's spot service-linked role; and
// in each region a security group and the G and VT quotas one instance needs,
// requested where short — and with a user named, that user, its key written to
// a file when it is created, and the policy attached to it. The regions are
// those named, or else every region the account has enabled that offers one of
// the instance types.
type Setup struct {
	// Account, where given, is the only account setup acts in.
	Account string
	// User is the operator user, and KeyFile where its key goes when it is
	// created; no user is set up without one.
	User, KeyFile string

	names Names
	// regions are the regions named; none, every one offering a type.
	regions []string
	// types are the instance types the policy admits.
	types []string
	log   func(string)

	global
	regional func(region string) regional
}

// NewSetup is a setup of the deployment names name in the regions — none,
// every one offering a type — its policy admitting the instance types, acting
// as cfg's identity and saying what it does through log.
func NewSetup(cfg awssdk.Config, names Names, regions, types []string, log func(string)) *Setup {
	return &Setup{
		names:    names,
		regions:  regions,
		types:    types,
		log:      log,
		global:   newGlobal(cfg),
		regional: func(region string) regional { return newRegional(cfg, region) },
	}
}

// regionName is what an AWS region is called: eu-north-1, ap-southeast-7.
var regionName = regexp.MustCompile(`^[a-z]+(-[a-z]+)+-[0-9]+$`)

// Converge brings the account's side in line, creating what is missing,
// replacing a policy that drifted and requesting quotas that are short. A
// region named wrong is refused before anything is: it would be written into
// the policy. A region that cannot be reached to see what it offers is left
// out with a warning: a run rents in the others.
func (s *Setup) Converge(ctx context.Context) error {
	for _, region := range s.regions {
		if !regionName.MatchString(region) {
			return fmt.Errorf("%q is no AWS region", region)
		}
	}
	account, err := callerAccount(ctx, s.sts)
	if err != nil {
		return err
	}
	if s.Account != "" && account != s.Account {
		return fmt.Errorf("these credentials are account %s, not %s", account, s.Account)
	}
	regions := s.regions
	if len(regions) == 0 {
		if regions, err = s.offeringRegions(ctx); err != nil {
			return err
		}
		if len(regions) == 0 {
			return fmt.Errorf("no region the account has enabled offers %s", strings.Join(s.types, ", "))
		}
	}
	s.log(fmt.Sprintf("translation GPU: AWS setup of %s in account %s, in %s", s.names, account, strings.Join(regions, ", ")))

	if s.User != "" {
		if err := s.user(ctx); err != nil {
			return err
		}
	}
	if err := s.roles(ctx); err != nil {
		return err
	}
	arn := s.names.PolicyARN(account)
	if err := s.policy(ctx, arn, operatorPolicy(account, s.names, regions, s.types)); err != nil {
		return err
	}
	if s.User != "" {
		if _, err := s.iam.AttachUserPolicy(ctx, &iam.AttachUserPolicyInput{UserName: awssdk.String(s.User), PolicyArn: awssdk.String(arn)}); err != nil {
			return err
		}
	}
	for _, region := range regions {
		if err := s.region(ctx, region); err != nil {
			return fmt.Errorf("%s: %w", region, err)
		}
	}
	return nil
}

// offeringRegions are the regions the account has enabled that offer one of
// the instance types, looked at all at once; each that could not be reached
// is warned of.
func (s *Setup) offeringRegions(ctx context.Context) ([]string, error) {
	enabled, err := s.regional(homeRegion).ec2.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return nil, err
	}
	names := make([]string, len(enabled.Regions))
	for i, region := range enabled.Regions {
		names[i] = awssdk.ToString(region.RegionName)
	}
	offering, failures, err := eachRegion(ctx, names, func(region string) (bool, error) {
		zones, _, err := offered(ctx, s.regional(region).ec2, s.types)
		return len(zones) > 0, err
	})
	if err != nil {
		return nil, err
	}
	var regions []string
	for i, name := range names {
		switch {
		case failures[i] != nil:
			s.log(fmt.Sprintf("WARNING %s: not reached, so not prepared — run setup again to have it, should it offer a type: %v", name, failures[i]))
		case offering[i]:
			regions = append(regions, name)
		}
	}
	slices.Sort(regions)
	return regions, nil
}

// user creates the operator user where it is missing, writing its key.
func (s *Setup) user(ctx context.Context) error {
	_, err := s.iam.GetUser(ctx, &iam.GetUserInput{UserName: awssdk.String(s.User)})
	if absent, err := missing(err); err != nil || !absent {
		return err
	}
	if _, err := s.iam.CreateUser(ctx, &iam.CreateUserInput{UserName: awssdk.String(s.User)}); err != nil {
		return err
	}
	key, err := s.iam.CreateAccessKey(ctx, &iam.CreateAccessKeyInput{UserName: awssdk.String(s.User)})
	if err != nil {
		return err
	}
	body := fmt.Sprintf("AWS_ACCESS_KEY_ID=%s\nAWS_SECRET_ACCESS_KEY=%s\n", awssdk.ToString(key.AccessKey.AccessKeyId), awssdk.ToString(key.AccessKey.SecretAccessKey))
	if err := os.WriteFile(s.KeyFile, []byte(body), 0o600); err != nil {
		return fmt.Errorf("user %s created, its key not written: %w", s.User, err)
	}
	s.log(fmt.Sprintf("user %s created, its key written to %s", s.User, s.KeyFile))
	return nil
}

// roles creates the instance's role and profile — registration with Systems
// Manager, which the tunnel runs over, and nothing else — and EC2's spot
// service-linked role, which a spot launch through the API needs to exist
// already, where any is missing.
func (s *Setup) roles(ctx context.Context) error {
	role := s.names.InstanceProfile()
	trust, err := json.Marshal(map[string]any{
		"Version":   "2012-10-17",
		"Statement": []map[string]any{{"Effect": "Allow", "Principal": map[string]string{"Service": "ec2.amazonaws.com"}, "Action": "sts:AssumeRole"}},
	})
	if err != nil {
		return err
	}
	for _, r := range []struct {
		what    string
		probe   func() error
		creates []func() error
	}{
		{"role " + role,
			func() error {
				_, err := s.iam.GetRole(ctx, &iam.GetRoleInput{RoleName: awssdk.String(role)})
				return err
			},
			[]func() error{
				func() error {
					_, err := s.iam.CreateRole(ctx, &iam.CreateRoleInput{RoleName: awssdk.String(role), AssumeRolePolicyDocument: awssdk.String(string(trust))})
					return err
				},
				func() error {
					_, err := s.iam.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{RoleName: awssdk.String(role), PolicyArn: awssdk.String("arn:" + partition + ":iam::aws:policy/AmazonSSMManagedInstanceCore")})
					return err
				},
			}},
		{"instance profile " + role,
			func() error {
				_, err := s.iam.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{InstanceProfileName: awssdk.String(role)})
				return err
			},
			[]func() error{
				func() error {
					_, err := s.iam.CreateInstanceProfile(ctx, &iam.CreateInstanceProfileInput{InstanceProfileName: awssdk.String(role)})
					return err
				},
				func() error {
					_, err := s.iam.AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{InstanceProfileName: awssdk.String(role), RoleName: awssdk.String(role)})
					return err
				},
			}},
		{"role AWSServiceRoleForEC2Spot",
			func() error {
				_, err := s.iam.GetRole(ctx, &iam.GetRoleInput{RoleName: awssdk.String("AWSServiceRoleForEC2Spot")})
				return err
			},
			[]func() error{
				func() error {
					_, err := s.iam.CreateServiceLinkedRole(ctx, &iam.CreateServiceLinkedRoleInput{AWSServiceName: awssdk.String("spot.amazonaws.com")})
					return err
				},
			}},
	} {
		absent, err := missing(r.probe())
		if err != nil {
			return err
		}
		if !absent {
			continue
		}
		for _, create := range r.creates {
			if err := create(); err != nil {
				return err
			}
		}
		s.log(r.what + " created")
	}
	return nil
}

// policy creates the operator policy, or gives a drifted one a new default
// version, dropping the oldest other version where IAM keeps no more.
func (s *Setup) policy(ctx context.Context, arn string, want policy) error {
	document, err := policyDocument(ctx, s.iam, arn)
	absent, err := missing(err)
	if err != nil {
		return err
	}
	if absent {
		if _, err := s.iam.CreatePolicy(ctx, &iam.CreatePolicyInput{PolicyName: awssdk.String(s.names.Policy()), PolicyDocument: awssdk.String(want.document())}); err != nil {
			return err
		}
		s.log(fmt.Sprintf("policy %s created", s.names.Policy()))
		return nil
	}
	same, err := want.sameDocument(document)
	if err != nil || same {
		return err
	}
	listed, err := s.iam.ListPolicyVersions(ctx, &iam.ListPolicyVersionsInput{PolicyArn: awssdk.String(arn)})
	if err != nil {
		return err
	}
	if versions := listed.Versions; len(versions) >= maxPolicyVersions {
		var oldest *iamtypes.PolicyVersion
		for i, v := range versions {
			if !v.IsDefaultVersion && (oldest == nil || v.CreateDate.Before(*oldest.CreateDate)) {
				oldest = &versions[i]
			}
		}
		if _, err := s.iam.DeletePolicyVersion(ctx, &iam.DeletePolicyVersionInput{PolicyArn: awssdk.String(arn), VersionId: oldest.VersionId}); err != nil {
			return err
		}
	}
	created, err := s.iam.CreatePolicyVersion(ctx, &iam.CreatePolicyVersionInput{PolicyArn: awssdk.String(arn), PolicyDocument: awssdk.String(want.document()), SetAsDefault: true})
	if err != nil {
		return err
	}
	s.log(fmt.Sprintf("policy %s is now %s", s.names.Policy(), awssdk.ToString(created.PolicyVersion.VersionId)))
	return nil
}

// region gives a region its security group — in the default VPC, with no
// ingress rule: Systems Manager, Docker Hub and Hugging Face are all reached
// outbound — and the G and VT quotas, spot and on-demand, of one instance of
// the largest type it offers, requesting those short; and reports where the
// instance types are offered and the quotas.
func (s *Setup) region(ctx context.Context, region string) error {
	clients := s.regional(region)
	groups, err := securityGroups(ctx, clients.ec2, s.names)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		if _, err := clients.ec2.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
			GroupName:   awssdk.String(s.names.SecurityGroup()),
			Description: awssdk.String("translation GPU - egress only"),
		}); err != nil {
			return err
		}
		s.log(fmt.Sprintf("%s: security group %s created", region, s.names.SecurityGroup()))
	}
	o, err := offeringIn(ctx, clients, s.types)
	if err != nil {
		return err
	}
	if len(o.zones) == 0 {
		s.log(fmt.Sprintf("WARNING %s: %s offered in no zone — a run finds nothing to launch there", region, strings.Join(s.types, ", ")))
		return nil
	}
	var held []string
	for _, quota := range quotas {
		state := fmt.Sprintf("%s %g vCPU", quota.market, o.quotas[quota.market])
		if !o.holds(quota.market) {
			requested, err := s.request(ctx, clients.quotas, quota.code, o.need)
			if err != nil {
				return err
			}
			state += " (" + requested + ")"
		}
		held = append(held, state)
	}
	s.log(fmt.Sprintf("%s: %s in %s; G and VT quotas %s", region, strings.Join(o.types, ", "), strings.Join(slices.Sorted(maps.Keys(o.zones)), ", "), strings.Join(held, ", ")))
	return nil
}

// request asks for a quota to be raised to want vCPU, unless a request of it
// is still open, and says which.
func (s *Setup) request(ctx context.Context, client quotasAPI, code string, want float64) (string, error) {
	for _, status := range []sqtypes.RequestStatus{sqtypes.RequestStatusPending, sqtypes.RequestStatusCaseOpened} {
		open, err := client.ListRequestedServiceQuotaChangeHistoryByQuota(ctx, &servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput{
			ServiceCode: awssdk.String("ec2"), QuotaCode: awssdk.String(code), Status: status,
		})
		if err != nil {
			return "", err
		}
		if len(open.RequestedQuotas) > 0 {
			return fmt.Sprintf("%g requested, %s", awssdk.ToFloat64(open.RequestedQuotas[0].DesiredValue), status), nil
		}
	}
	if _, err := client.RequestServiceQuotaIncrease(ctx, &servicequotas.RequestServiceQuotaIncreaseInput{
		ServiceCode: awssdk.String("ec2"), QuotaCode: awssdk.String(code), DesiredValue: awssdk.Float64(want),
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%g requested now", want), nil
}
