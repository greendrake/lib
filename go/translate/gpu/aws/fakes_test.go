package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	sqtypes "github.com/aws/aws-sdk-go-v2/service/servicequotas/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// noCapacity is the error a zone refuses a launch with when it has no
// instance of the type to give.
const noCapacity = "InsufficientInstanceCapacity"

// fakeEC2 is a region as EC2 answers it: the regions it lists, what is offered
// and priced where, the zones refusing a launch and with which error, the
// instances of earlier leases, and the one a lease launches — whose states it
// steps through, one per look. With unreachable set, what is asked first of a
// region fails.
type fakeEC2 struct {
	mu            sync.Mutex
	regions       []string
	unreachable   error
	groups        []string
	offered       []string
	subnets       map[string]string
	spotPrices    map[string]string
	refusing      map[string]string
	previous      []ec2types.Instance
	openRequests  [][]string
	states        []ec2types.InstanceStateName
	stateReason   string
	consoles      []string
	launched      []*ec2.RunInstancesInput
	terminated    []string
	createdGroups []string
	looks         int
}

func (f *fakeEC2) DescribeRegions(_ context.Context, _ *ec2.DescribeRegionsInput, _ ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error) {
	out := &ec2.DescribeRegionsOutput{}
	for _, region := range f.regions {
		out.Regions = append(out.Regions, ec2types.Region{RegionName: awssdk.String(region)})
	}
	return out, nil
}

// DescribeInstanceTypes says every type takes 8 vCPU, as g7e.2xlarge does.
func (f *fakeEC2) DescribeInstanceTypes(_ context.Context, in *ec2.DescribeInstanceTypesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	out := &ec2.DescribeInstanceTypesOutput{}
	for _, t := range in.InstanceTypes {
		out.InstanceTypes = append(out.InstanceTypes, ec2types.InstanceTypeInfo{InstanceType: t, VCpuInfo: &ec2types.VCpuInfo{DefaultVCpus: awssdk.Int32(8)}})
	}
	return out, nil
}

func (f *fakeEC2) DescribeSecurityGroups(_ context.Context, in *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &ec2.DescribeSecurityGroupsOutput{}
	for _, id := range f.groups {
		out.SecurityGroups = append(out.SecurityGroups, ec2types.SecurityGroup{GroupId: awssdk.String(id), GroupName: awssdk.String(in.Filters[0].Values[0])})
	}
	return out, nil
}

func (f *fakeEC2) CreateSecurityGroup(_ context.Context, in *ec2.CreateSecurityGroupInput, _ ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdGroups = append(f.createdGroups, awssdk.ToString(in.GroupName))
	return &ec2.CreateSecurityGroupOutput{GroupId: awssdk.String("sg-new")}, nil
}

// DescribeInstanceTypeOfferings offers the first type asked for in the
// offered zones.
func (f *fakeEC2) DescribeInstanceTypeOfferings(_ context.Context, in *ec2.DescribeInstanceTypeOfferingsInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypeOfferingsOutput, error) {
	if f.unreachable != nil {
		return nil, f.unreachable
	}
	out := &ec2.DescribeInstanceTypeOfferingsOutput{}
	for _, zone := range f.offered {
		out.InstanceTypeOfferings = append(out.InstanceTypeOfferings, ec2types.InstanceTypeOffering{Location: awssdk.String(zone), InstanceType: ec2types.InstanceType(in.Filters[0].Values[0])})
	}
	return out, nil
}

func (f *fakeEC2) DescribeSubnets(_ context.Context, _ *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	out := &ec2.DescribeSubnetsOutput{}
	for zone, subnet := range f.subnets {
		out.Subnets = append(out.Subnets, ec2types.Subnet{AvailabilityZone: awssdk.String(zone), SubnetId: awssdk.String(subnet)})
	}
	return out, nil
}

// DescribeInstances answers by id with the launched instance's next state,
// and by filter with the earlier leases' instances.
func (f *fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(in.InstanceIds) == 0 {
		if f.unreachable != nil {
			return nil, f.unreachable
		}
		return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: f.previous}}}, nil
	}
	state := f.states[min(f.looks, len(f.states)-1)]
	f.looks++
	instance := ec2types.Instance{InstanceId: awssdk.String(in.InstanceIds[0]), State: &ec2types.InstanceState{Name: state}}
	if f.stateReason != "" {
		instance.StateReason = &ec2types.StateReason{Message: awssdk.String(f.stateReason)}
	}
	return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{instance}}}}, nil
}

func (f *fakeEC2) DescribeSpotInstanceRequests(_ context.Context, _ *ec2.DescribeSpotInstanceRequestsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSpotInstanceRequestsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &ec2.DescribeSpotInstanceRequestsOutput{}
	if len(f.openRequests) > 0 {
		for _, id := range f.openRequests[0] {
			out.SpotInstanceRequests = append(out.SpotInstanceRequests, ec2types.SpotInstanceRequest{SpotInstanceRequestId: awssdk.String(id)})
		}
		f.openRequests = f.openRequests[1:]
	}
	return out, nil
}

func (f *fakeEC2) DescribeSpotPriceHistory(_ context.Context, _ *ec2.DescribeSpotPriceHistoryInput, _ ...func(*ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
	out := &ec2.DescribeSpotPriceHistoryOutput{}
	for zone, price := range f.spotPrices {
		out.SpotPriceHistory = append(out.SpotPriceHistory, ec2types.SpotPrice{AvailabilityZone: awssdk.String(zone), SpotPrice: awssdk.String(price)})
	}
	return out, nil
}

func (f *fakeEC2) RunInstances(_ context.Context, in *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launched = append(f.launched, in)
	zone := zoneOf(f.subnets, awssdk.ToString(in.SubnetId))
	if code, ok := f.refusing[zone]; ok {
		return nil, &smithy.GenericAPIError{Code: code, Message: "refused in " + zone}
	}
	return &ec2.RunInstancesOutput{Instances: []ec2types.Instance{{InstanceId: awssdk.String("i-" + zone)}}}, nil
}

func (f *fakeEC2) GetConsoleOutput(_ context.Context, _ *ec2.GetConsoleOutputInput, _ ...func(*ec2.Options)) (*ec2.GetConsoleOutputOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var output string
	if len(f.consoles) > 0 {
		output = f.consoles[0]
		if len(f.consoles) > 1 {
			f.consoles = f.consoles[1:]
		}
	}
	return &ec2.GetConsoleOutputOutput{Output: awssdk.String(base64.StdEncoding.EncodeToString([]byte(output)))}, nil
}

func (f *fakeEC2) TerminateInstances(_ context.Context, in *ec2.TerminateInstancesInput, _ ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !awssdk.ToBool(in.SkipOsShutdown) {
		return nil, errors.New("the lease terminates without the OS's shutdown")
	}
	f.terminated = append(f.terminated, in.InstanceIds...)
	return &ec2.TerminateInstancesOutput{TerminatingInstances: []ec2types.InstanceStateChange{{CurrentState: &ec2types.InstanceState{Name: ec2types.InstanceStateNameShuttingDown}}}}, nil
}

func zoneOf(subnets map[string]string, subnet string) string {
	for zone, id := range subnets {
		if id == subnet {
			return zone
		}
	}
	return ""
}

// launchedZones are the zones RunInstances was asked for, in order.
func (f *fakeEC2) launchedZones() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var zones []string
	for _, in := range f.launched {
		zones = append(zones, zoneOf(f.subnets, awssdk.ToString(in.SubnetId)))
	}
	return zones
}

func (f *fakeEC2) terminatedInstances() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.terminated)
}

// fakeSSM resolves the AMI and has the instance's agent come online.
type fakeSSM struct{}

func (fakeSSM) GetParameter(_ context.Context, in *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	if awssdk.ToString(in.Name) != dlami {
		return nil, fmt.Errorf("no parameter %s", awssdk.ToString(in.Name))
	}
	return &ssm.GetParameterOutput{Parameter: &ssmtypes.Parameter{Value: awssdk.String("ami-dlami")}}, nil
}

func (fakeSSM) DescribeInstanceInformation(_ context.Context, _ *ssm.DescribeInstanceInformationInput, _ ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error) {
	return &ssm.DescribeInstanceInformationOutput{InstanceInformationList: []ssmtypes.InstanceInformation{{PingStatus: ssmtypes.PingStatusOnline}}}, nil
}

func (fakeSSM) StartSession(_ context.Context, in *ssm.StartSessionInput, _ ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	return &ssm.StartSessionOutput{SessionId: awssdk.String("user-0abc"), TokenValue: awssdk.String("token"), StreamUrl: awssdk.String("wss://ssmmessages.example/" + awssdk.ToString(in.Target))}, nil
}

func (fakeSSM) TerminateSession(_ context.Context, _ *ssm.TerminateSessionInput, _ ...func(*ssm.Options)) (*ssm.TerminateSessionOutput, error) {
	return &ssm.TerminateSessionOutput{}, nil
}

// fakePricing lists, by region, the one on-demand product it has there.
type fakePricing map[string]string

func (f fakePricing) GetProducts(_ context.Context, in *pricing.GetProductsInput, _ ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
	out := &pricing.GetProductsOutput{}
	for _, filter := range in.Filters {
		if product, ok := f[awssdk.ToString(filter.Value)]; ok && awssdk.ToString(filter.Field) == "regionCode" {
			out.PriceList = append(out.PriceList, product)
		}
	}
	return out, nil
}

// fakeTunnels open tunnels to a stand-in for vLLM, refusing the first few.
type fakeTunnels struct {
	mu       sync.Mutex
	notReady error
	refusals int
	server   *httptest.Server
	opened   []*fakeTunnel
	regions  []string
}

func (f *fakeTunnels) ready() error { return f.notReady }

func (f *fakeTunnels) open(_ context.Context, region, instance string) (tunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.regions = append(f.regions, region)
	if f.refusals > 0 {
		f.refusals--
		return nil, &smithy.GenericAPIError{Code: "TargetNotConnected", Message: instance + " is not connected."}
	}
	t := &fakeTunnel{local: f.server.URL}
	f.opened = append(f.opened, t)
	return t, nil
}

func (f *fakeTunnels) latest() *fakeTunnel {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opened[len(f.opened)-1]
}

type fakeTunnel struct {
	mu        sync.Mutex
	local     string
	gone      bool
	wasClosed bool
}

func (t *fakeTunnel) url() string { return t.local }

func (t *fakeTunnel) closed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gone
}

func (t *fakeTunnel) reason() string { return "Cannot perform start session: EOF" }

func (t *fakeTunnel) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.wasClosed = true
}

func (t *fakeTunnel) drop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gone = true
}

// healthy is a stand-in for vLLM answering its /health.
func healthy(t *testing.T) *httptest.Server { return healthyAfter(t, 0) }

// healthyAfter is a stand-in for vLLM loading through its first looks.
func healthyAfter(t *testing.T, looks int32) *httptest.Server {
	t.Helper()
	var seen atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path != "/health":
			http.NotFound(w, r)
		case seen.Add(1) <= looks:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// lines collects what is logged.
type lines struct {
	mu  sync.Mutex
	all []string
}

func (l *lines) log(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all = append(l.all, line)
}

func (l *lines) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.all)
}

// region is eu-north-1, three zones offering the type at three spot prices,
// its instance pending, then running.
func region() *fakeEC2 {
	return &fakeEC2{
		groups:     []string{"sg-1"},
		offered:    []string{"eu-north-1a", "eu-north-1b", "eu-north-1c"},
		subnets:    map[string]string{"eu-north-1a": "subnet-a", "eu-north-1b": "subnet-b", "eu-north-1c": "subnet-c"},
		spotPrices: map[string]string{"eu-north-1a": "0.912000", "eu-north-1b": "0.611000", "eu-north-1c": "0.744000"},
		states:     []ec2types.InstanceStateName{ec2types.InstanceStateNamePending, ec2types.InstanceStateNameRunning},
	}
}

// west is us-west-2, two zones offering the type, one cheaper than any of
// eu-north-1's and one between them.
func west() *fakeEC2 {
	return &fakeEC2{
		groups:     []string{"sg-w"},
		offered:    []string{"us-west-2a", "us-west-2b"},
		subnets:    map[string]string{"us-west-2a": "subnet-wa", "us-west-2b": "subnet-wb"},
		spotPrices: map[string]string{"us-west-2a": "0.500000", "us-west-2b": "0.700000"},
		states:     []ec2types.InstanceStateName{ec2types.InstanceStateNamePending, ec2types.InstanceStateNameRunning},
	}
}

const (
	onDemandProduct     = `{"product":{"attributes":{"instanceType":"g7e.2xlarge"}},"terms":{"OnDemand":{"ABC.JRTCKXETXF":{"priceDimensions":{"ABC.JRTCKXETXF.6YS6EN2CT7":{"pricePerUnit":{"USD":"3.1416000000"}}}}}}}`
	westOnDemandProduct = `{"product":{"attributes":{"instanceType":"g7e.2xlarge"}},"terms":{"OnDemand":{"ABC.JRTCKXETXF":{"priceDimensions":{"ABC.JRTCKXETXF.6YS6EN2CT7":{"pricePerUnit":{"USD":"2.7183000000"}}}}}}}`
)

// lease is a lease of the built-in profile from the fake regions, each with
// the quotas for spot and on-demand alike, all of them prepared: the operator
// policy confines the operator to them.
func lease(ec2 map[string]*fakeEC2, tunnels *fakeTunnels, log *lines) *Lease {
	quotas := map[string]*fakeQuotas{}
	for region := range ec2 {
		quotas[region] = quotasOf(8, 8)
	}
	return leaseWith(ec2, quotas, tunnels, log)
}

// leaseWith is lease with each region's quotas as given.
func leaseWith(ec2 map[string]*fakeEC2, quotas map[string]*fakeQuotas, tunnels *fakeTunnels, log *lines) *Lease {
	iam := converged()
	iam.document = url.PathEscape(operatorPolicy(account, "acme-translate", slices.Sorted(maps.Keys(ec2)), []string{"g7e.2xlarge"}).document())
	return &Lease{
		names: "acme-translate", profile: builtins["gemma4-31b"], log: log.log,
		global: global{iam: iam, sts: fakeSTS{account: account}, pricing: fakePricing{"eu-north-1": onDemandProduct, "us-west-2": westOnDemandProduct}},
		regional: func(region string) regional {
			return regional{ec2: ec2[region], ssm: fakeSSM{}, quotas: quotas[region]}
		},
		tunnels: tunnels, poll: time.Millisecond,
	}
}

// north is eu-north-1 alone.
func north(ec2 *fakeEC2) map[string]*fakeEC2 { return map[string]*fakeEC2{"eu-north-1": ec2} }

// fakeIAM is an account's IAM: what exists, and what setup did.
type fakeIAM struct {
	users, roles, profiles map[string]bool
	policy                 string
	versions               []iamtypes.PolicyVersion
	document               string
	calls                  []string
}

func (f *fakeIAM) called(call string) { f.calls = append(f.calls, call) }

func noSuch() error { return &iamtypes.NoSuchEntityException{Message: awssdk.String("not found")} }

func (f *fakeIAM) GetUser(_ context.Context, in *iam.GetUserInput, _ ...func(*iam.Options)) (*iam.GetUserOutput, error) {
	if !f.users[awssdk.ToString(in.UserName)] {
		return nil, noSuch()
	}
	return &iam.GetUserOutput{}, nil
}

func (f *fakeIAM) CreateUser(_ context.Context, in *iam.CreateUserInput, _ ...func(*iam.Options)) (*iam.CreateUserOutput, error) {
	f.called("CreateUser " + awssdk.ToString(in.UserName))
	return &iam.CreateUserOutput{}, nil
}

func (f *fakeIAM) CreateAccessKey(_ context.Context, in *iam.CreateAccessKeyInput, _ ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error) {
	f.called("CreateAccessKey " + awssdk.ToString(in.UserName))
	return &iam.CreateAccessKeyOutput{AccessKey: &iamtypes.AccessKey{AccessKeyId: awssdk.String("AKIAEXAMPLE"), SecretAccessKey: awssdk.String("secret/example")}}, nil
}

func (f *fakeIAM) AttachUserPolicy(_ context.Context, in *iam.AttachUserPolicyInput, _ ...func(*iam.Options)) (*iam.AttachUserPolicyOutput, error) {
	f.called("AttachUserPolicy " + awssdk.ToString(in.UserName) + " " + awssdk.ToString(in.PolicyArn))
	return &iam.AttachUserPolicyOutput{}, nil
}

func (f *fakeIAM) GetRole(_ context.Context, in *iam.GetRoleInput, _ ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	if !f.roles[awssdk.ToString(in.RoleName)] {
		return nil, noSuch()
	}
	return &iam.GetRoleOutput{}, nil
}

func (f *fakeIAM) CreateRole(_ context.Context, in *iam.CreateRoleInput, _ ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	f.called("CreateRole " + awssdk.ToString(in.RoleName) + " " + awssdk.ToString(in.AssumeRolePolicyDocument))
	return &iam.CreateRoleOutput{}, nil
}

func (f *fakeIAM) AttachRolePolicy(_ context.Context, in *iam.AttachRolePolicyInput, _ ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
	f.called("AttachRolePolicy " + awssdk.ToString(in.RoleName) + " " + awssdk.ToString(in.PolicyArn))
	return &iam.AttachRolePolicyOutput{}, nil
}

func (f *fakeIAM) GetInstanceProfile(_ context.Context, in *iam.GetInstanceProfileInput, _ ...func(*iam.Options)) (*iam.GetInstanceProfileOutput, error) {
	if !f.profiles[awssdk.ToString(in.InstanceProfileName)] {
		return nil, noSuch()
	}
	return &iam.GetInstanceProfileOutput{}, nil
}

func (f *fakeIAM) CreateInstanceProfile(_ context.Context, in *iam.CreateInstanceProfileInput, _ ...func(*iam.Options)) (*iam.CreateInstanceProfileOutput, error) {
	f.called("CreateInstanceProfile " + awssdk.ToString(in.InstanceProfileName))
	return &iam.CreateInstanceProfileOutput{}, nil
}

func (f *fakeIAM) AddRoleToInstanceProfile(_ context.Context, in *iam.AddRoleToInstanceProfileInput, _ ...func(*iam.Options)) (*iam.AddRoleToInstanceProfileOutput, error) {
	f.called("AddRoleToInstanceProfile " + awssdk.ToString(in.InstanceProfileName) + " " + awssdk.ToString(in.RoleName))
	return &iam.AddRoleToInstanceProfileOutput{}, nil
}

func (f *fakeIAM) CreateServiceLinkedRole(_ context.Context, in *iam.CreateServiceLinkedRoleInput, _ ...func(*iam.Options)) (*iam.CreateServiceLinkedRoleOutput, error) {
	f.called("CreateServiceLinkedRole " + awssdk.ToString(in.AWSServiceName))
	return &iam.CreateServiceLinkedRoleOutput{}, nil
}

func (f *fakeIAM) GetPolicy(_ context.Context, in *iam.GetPolicyInput, _ ...func(*iam.Options)) (*iam.GetPolicyOutput, error) {
	if f.policy != awssdk.ToString(in.PolicyArn) {
		return nil, noSuch()
	}
	current := slices.IndexFunc(f.versions, func(v iamtypes.PolicyVersion) bool { return v.IsDefaultVersion })
	return &iam.GetPolicyOutput{Policy: &iamtypes.Policy{DefaultVersionId: f.versions[current].VersionId}}, nil
}

func (f *fakeIAM) CreatePolicy(_ context.Context, in *iam.CreatePolicyInput, _ ...func(*iam.Options)) (*iam.CreatePolicyOutput, error) {
	f.called("CreatePolicy " + awssdk.ToString(in.PolicyName))
	f.document = awssdk.ToString(in.PolicyDocument)
	return &iam.CreatePolicyOutput{}, nil
}

func (f *fakeIAM) ListPolicyVersions(_ context.Context, _ *iam.ListPolicyVersionsInput, _ ...func(*iam.Options)) (*iam.ListPolicyVersionsOutput, error) {
	return &iam.ListPolicyVersionsOutput{Versions: f.versions}, nil
}

func (f *fakeIAM) GetPolicyVersion(_ context.Context, in *iam.GetPolicyVersionInput, _ ...func(*iam.Options)) (*iam.GetPolicyVersionOutput, error) {
	return &iam.GetPolicyVersionOutput{PolicyVersion: &iamtypes.PolicyVersion{VersionId: in.VersionId, Document: awssdk.String(f.document)}}, nil
}

func (f *fakeIAM) DeletePolicyVersion(_ context.Context, in *iam.DeletePolicyVersionInput, _ ...func(*iam.Options)) (*iam.DeletePolicyVersionOutput, error) {
	f.called("DeletePolicyVersion " + awssdk.ToString(in.VersionId))
	return &iam.DeletePolicyVersionOutput{}, nil
}

func (f *fakeIAM) CreatePolicyVersion(_ context.Context, in *iam.CreatePolicyVersionInput, _ ...func(*iam.Options)) (*iam.CreatePolicyVersionOutput, error) {
	f.called(fmt.Sprintf("CreatePolicyVersion default=%t", in.SetAsDefault))
	f.document = awssdk.ToString(in.PolicyDocument)
	return &iam.CreatePolicyVersionOutput{PolicyVersion: &iamtypes.PolicyVersion{VersionId: awssdk.String(fmt.Sprintf("v%d", len(f.versions)+1))}}, nil
}

// fakeSTS is the identity setup and a lease act as.
type fakeSTS struct{ account string }

func (f fakeSTS) GetCallerIdentity(_ context.Context, _ *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return &sts.GetCallerIdentityOutput{
		Account: awssdk.String(f.account),
		Arn:     awssdk.String(fmt.Sprintf("arn:aws:iam::%s:user/admin", f.account)),
	}, nil
}

// fakeQuotas is a region's quotas, in vCPU, by code; the requests of them
// still open, by code; and those made.
type fakeQuotas struct {
	mu        sync.Mutex
	values    map[string]float64
	open      map[string]sqtypes.RequestStatus
	requested []string
}

// quotasOf are a region's G and VT quotas, spot and on-demand.
func quotasOf(spot, onDemand float64) *fakeQuotas {
	return &fakeQuotas{values: map[string]float64{"L-3819A6DF": spot, "L-DB2E81BA": onDemand}}
}

func (f *fakeQuotas) GetServiceQuota(_ context.Context, in *servicequotas.GetServiceQuotaInput, _ ...func(*servicequotas.Options)) (*servicequotas.GetServiceQuotaOutput, error) {
	return &servicequotas.GetServiceQuotaOutput{Quota: &sqtypes.ServiceQuota{Value: awssdk.Float64(f.values[awssdk.ToString(in.QuotaCode)])}}, nil
}

func (f *fakeQuotas) ListRequestedServiceQuotaChangeHistoryByQuota(_ context.Context, in *servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput, _ ...func(*servicequotas.Options)) (*servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput, error) {
	out := &servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput{}
	if status, ok := f.open[awssdk.ToString(in.QuotaCode)]; ok && status == in.Status {
		out.RequestedQuotas = []sqtypes.RequestedServiceQuotaChange{{DesiredValue: awssdk.Float64(8), Status: status}}
	}
	return out, nil
}

func (f *fakeQuotas) RequestServiceQuotaIncrease(_ context.Context, in *servicequotas.RequestServiceQuotaIncreaseInput, _ ...func(*servicequotas.Options)) (*servicequotas.RequestServiceQuotaIncreaseOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requested = append(f.requested, fmt.Sprintf("%s=%g", awssdk.ToString(in.QuotaCode), awssdk.ToFloat64(in.DesiredValue)))
	return &servicequotas.RequestServiceQuotaIncreaseOutput{}, nil
}
