package aws

import (
	"context"
	"errors"
	"slices"
	"sync"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// What of AWS the command calls, each service narrowed to the calls it makes:
// the SDK's clients satisfy them, and the tests' fakes do.

type ec2API interface {
	DescribeRegions(context.Context, *ec2.DescribeRegionsInput, ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error)
	DescribeSecurityGroups(context.Context, *ec2.DescribeSecurityGroupsInput, ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	CreateSecurityGroup(context.Context, *ec2.CreateSecurityGroupInput, ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error)
	DescribeInstanceTypeOfferings(context.Context, *ec2.DescribeInstanceTypeOfferingsInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceTypeOfferingsOutput, error)
	DescribeInstanceTypes(context.Context, *ec2.DescribeInstanceTypesInput, ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error)
	DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeSpotInstanceRequests(context.Context, *ec2.DescribeSpotInstanceRequestsInput, ...func(*ec2.Options)) (*ec2.DescribeSpotInstanceRequestsOutput, error)
	DescribeSpotPriceHistory(context.Context, *ec2.DescribeSpotPriceHistoryInput, ...func(*ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error)
	RunInstances(context.Context, *ec2.RunInstancesInput, ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	GetConsoleOutput(context.Context, *ec2.GetConsoleOutputInput, ...func(*ec2.Options)) (*ec2.GetConsoleOutputOutput, error)
	TerminateInstances(context.Context, *ec2.TerminateInstancesInput, ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
}

type ssmAPI interface {
	GetParameter(context.Context, *ssm.GetParameterInput, ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	DescribeInstanceInformation(context.Context, *ssm.DescribeInstanceInformationInput, ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error)
	StartSession(context.Context, *ssm.StartSessionInput, ...func(*ssm.Options)) (*ssm.StartSessionOutput, error)
	TerminateSession(context.Context, *ssm.TerminateSessionInput, ...func(*ssm.Options)) (*ssm.TerminateSessionOutput, error)
}

// policyReader reads a managed policy's document.
type policyReader interface {
	GetPolicy(context.Context, *iam.GetPolicyInput, ...func(*iam.Options)) (*iam.GetPolicyOutput, error)
	GetPolicyVersion(context.Context, *iam.GetPolicyVersionInput, ...func(*iam.Options)) (*iam.GetPolicyVersionOutput, error)
}

type iamAPI interface {
	policyReader
	GetUser(context.Context, *iam.GetUserInput, ...func(*iam.Options)) (*iam.GetUserOutput, error)
	CreateUser(context.Context, *iam.CreateUserInput, ...func(*iam.Options)) (*iam.CreateUserOutput, error)
	CreateAccessKey(context.Context, *iam.CreateAccessKeyInput, ...func(*iam.Options)) (*iam.CreateAccessKeyOutput, error)
	AttachUserPolicy(context.Context, *iam.AttachUserPolicyInput, ...func(*iam.Options)) (*iam.AttachUserPolicyOutput, error)
	GetRole(context.Context, *iam.GetRoleInput, ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	CreateRole(context.Context, *iam.CreateRoleInput, ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	AttachRolePolicy(context.Context, *iam.AttachRolePolicyInput, ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
	GetInstanceProfile(context.Context, *iam.GetInstanceProfileInput, ...func(*iam.Options)) (*iam.GetInstanceProfileOutput, error)
	CreateInstanceProfile(context.Context, *iam.CreateInstanceProfileInput, ...func(*iam.Options)) (*iam.CreateInstanceProfileOutput, error)
	AddRoleToInstanceProfile(context.Context, *iam.AddRoleToInstanceProfileInput, ...func(*iam.Options)) (*iam.AddRoleToInstanceProfileOutput, error)
	CreateServiceLinkedRole(context.Context, *iam.CreateServiceLinkedRoleInput, ...func(*iam.Options)) (*iam.CreateServiceLinkedRoleOutput, error)
	CreatePolicy(context.Context, *iam.CreatePolicyInput, ...func(*iam.Options)) (*iam.CreatePolicyOutput, error)
	ListPolicyVersions(context.Context, *iam.ListPolicyVersionsInput, ...func(*iam.Options)) (*iam.ListPolicyVersionsOutput, error)
	DeletePolicyVersion(context.Context, *iam.DeletePolicyVersionInput, ...func(*iam.Options)) (*iam.DeletePolicyVersionOutput, error)
	CreatePolicyVersion(context.Context, *iam.CreatePolicyVersionInput, ...func(*iam.Options)) (*iam.CreatePolicyVersionOutput, error)
}

type stsAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

type pricingAPI interface {
	GetProducts(context.Context, *pricing.GetProductsInput, ...func(*pricing.Options)) (*pricing.GetProductsOutput, error)
}

type quotasAPI interface {
	GetServiceQuota(context.Context, *servicequotas.GetServiceQuotaInput, ...func(*servicequotas.Options)) (*servicequotas.GetServiceQuotaOutput, error)
	ListRequestedServiceQuotaChangeHistoryByQuota(context.Context, *servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput, ...func(*servicequotas.Options)) (*servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaOutput, error)
	RequestServiceQuotaIncrease(context.Context, *servicequotas.RequestServiceQuotaIncreaseInput, ...func(*servicequotas.Options)) (*servicequotas.RequestServiceQuotaIncreaseOutput, error)
}

// regional are a region's clients.
type regional struct {
	ec2    ec2API
	ssm    ssmAPI
	quotas quotasAPI
}

func newRegional(cfg awssdk.Config, region string) regional {
	cfg.Region = region
	return regional{ec2: ec2.NewFromConfig(cfg), ssm: ssm.NewFromConfig(cfg), quotas: servicequotas.NewFromConfig(cfg)}
}

const (
	// partition is the AWS partition the command acts in: the commercial
	// one, whose home region below answers its calls about no one region.
	partition = "aws"
	// homeRegion is where the calls about no one region go: IAM and STS, the
	// list of the account's regions and the Pricing API, which answers there.
	homeRegion = "us-east-1"
)

// global are the clients of the calls about no one region.
type global struct {
	iam     iamAPI
	sts     stsAPI
	pricing pricingAPI
}

func newGlobal(cfg awssdk.Config) global {
	cfg.Region = homeRegion
	return global{iam: iam.NewFromConfig(cfg), sts: sts.NewFromConfig(cfg), pricing: pricing.NewFromConfig(cfg)}
}

// market is how an instance is bought: spot, or on demand.
type market string

const (
	spot     market = "spot"
	onDemand market = "on-demand"
)

// quotas are the quotas a launch draws on, in vCPU per region: running G and
// VT instances, by market.
var quotas = []struct {
	code   string
	market market
}{{"L-3819A6DF", spot}, {"L-DB2E81BA", onDemand}}

// offering is what a region offers of instance types: the zones offering any,
// those of the types offered there, and the region's G and VT quotas against
// the vCPUs one instance of the largest of them takes.
type offering struct {
	zones  map[string]bool
	types  []string
	need   float64
	quotas map[market]float64
}

// holds reports whether the region's quota on a market holds one instance.
func (o offering) holds(m market) bool { return o.quotas[m] >= o.need }

// offeringIn is a region's offering of the instance types; one offering none
// has no zones, and nothing more is read of it.
func offeringIn(ctx context.Context, clients regional, types []string) (offering, error) {
	zones, here, err := offered(ctx, clients.ec2, types)
	if err != nil || len(zones) == 0 {
		return offering{}, err
	}
	described, err := clients.ec2.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{InstanceTypes: toInstanceTypes(here)})
	if err != nil {
		return offering{}, err
	}
	o := offering{zones: zones, types: here, quotas: map[market]float64{}}
	for _, t := range described.InstanceTypes {
		o.need = max(o.need, float64(awssdk.ToInt32(t.VCpuInfo.DefaultVCpus)))
	}
	for _, quota := range quotas {
		got, err := clients.quotas.GetServiceQuota(ctx, &servicequotas.GetServiceQuotaInput{ServiceCode: awssdk.String("ec2"), QuotaCode: awssdk.String(quota.code)})
		if err != nil {
			return offering{}, err
		}
		o.quotas[quota.market] = awssdk.ToFloat64(got.Quota.Value)
	}
	return o, nil
}

// offered are the zones of a region offering any of the instance types, and
// those of the types offered there.
func offered(ctx context.Context, client ec2API, types []string) (map[string]bool, []string, error) {
	offerings, err := client.DescribeInstanceTypeOfferings(ctx, &ec2.DescribeInstanceTypeOfferingsInput{
		LocationType: ec2types.LocationTypeAvailabilityZone,
		Filters:      []ec2types.Filter{{Name: awssdk.String("instance-type"), Values: types}},
	})
	if err != nil {
		return nil, nil, err
	}
	zones := map[string]bool{}
	var here []string
	for _, offering := range offerings.InstanceTypeOfferings {
		zones[awssdk.ToString(offering.Location)] = true
		if t := string(offering.InstanceType); !slices.Contains(here, t) {
			here = append(here, t)
		}
	}
	slices.Sort(here)
	return zones, here, nil
}

func toInstanceTypes(types []string) []ec2types.InstanceType {
	out := make([]ec2types.InstanceType, len(types))
	for i, t := range types {
		out[i] = ec2types.InstanceType(t)
	}
	return out
}

// eachRegion asks of every region at once, and returns what each answered
// and why each that did not failed, in the regions' order — or, the context
// ended meanwhile, its error alone: a region's failure is then the
// interrupt's, not the region's.
func eachRegion[T any](ctx context.Context, regions []string, ask func(region string) (T, error)) ([]T, []error, error) {
	answers := make([]T, len(regions))
	failures := make([]error, len(regions))
	var wg sync.WaitGroup
	for i, region := range regions {
		wg.Go(func() { answers[i], failures[i] = ask(region) })
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return answers, failures, nil
}

// callerAccount is the account the credentials are in.
func callerAccount(ctx context.Context, client stsAPI) (string, error) {
	identity, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return awssdk.ToString(identity.Account), nil
}

// policyDocument is a managed policy's default version, URL-encoded as IAM
// returns it; a policy that does not exist is IAM's NoSuchEntity.
func policyDocument(ctx context.Context, client policyReader, arn string) (string, error) {
	got, err := client.GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: awssdk.String(arn)})
	if err != nil {
		return "", err
	}
	version, err := client.GetPolicyVersion(ctx, &iam.GetPolicyVersionInput{PolicyArn: awssdk.String(arn), VersionId: got.Policy.DefaultVersionId})
	if err != nil {
		return "", err
	}
	return awssdk.ToString(version.PolicyVersion.Document), nil
}

// securityGroups are a region's security groups of the deployment's name.
func securityGroups(ctx context.Context, client ec2API, names Names) ([]ec2types.SecurityGroup, error) {
	out, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{{Name: awssdk.String("group-name"), Values: []string{names.SecurityGroup()}}},
	})
	if err != nil {
		return nil, err
	}
	return out.SecurityGroups, nil
}

// errorCode is an AWS error's code, or "" for an error that is no AWS error.
func errorCode(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode()
	}
	return ""
}

// missing reports whether an IAM lookup found nothing; any other failure is
// its error.
func missing(err error) (bool, error) {
	var none *iamtypes.NoSuchEntityException
	if errors.As(err, &none) {
		return true, nil
	}
	return false, err
}
