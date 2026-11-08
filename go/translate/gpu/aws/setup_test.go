package aws

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	sqtypes "github.com/aws/aws-sdk-go-v2/service/servicequotas/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	account   = "123456789012"
	policyARN = "arn:aws:iam::123456789012:policy/acme-translate-operator"
)

var regions = []string{"eu-north-1", "eu-south-2"}

// setupOf is a setup in the named regions of a fake account whose regions
// each hold what ec2 says, with the quotas given.
func setupOf(iam *fakeIAM, ec2 map[string]*fakeEC2, quotas *fakeQuotas, log *lines) *Setup {
	return &Setup{
		names: "acme-translate", regions: regions, types: []string{"g7e.2xlarge"}, log: log.log,
		global:   global{iam: iam, sts: fakeSTS{account: account}},
		regional: func(region string) regional { return regional{ec2: ec2[region], quotas: quotas} },
	}
}

func bare() *fakeIAM {
	return &fakeIAM{users: map[string]bool{}, roles: map[string]bool{}, profiles: map[string]bool{}}
}

func converged() *fakeIAM {
	return &fakeIAM{
		users:    map[string]bool{"acme-translate": true},
		roles:    map[string]bool{"acme-translate-instance": true, "AWSServiceRoleForEC2Spot": true},
		profiles: map[string]bool{"acme-translate-instance": true},
		policy:   policyARN,
		versions: []iamtypes.PolicyVersion{{VersionId: awssdk.String("v1"), IsDefaultVersion: true, CreateDate: awssdk.Time(time.Unix(1, 0))}},
		document: url.PathEscape(operatorPolicy(account, "acme-translate", regions, []string{"g7e.2xlarge"}).document()),
	}
}

// An account with nothing in it gets everything a run needs: the operator
// user and its key, the instance's role and profile, EC2's spot role, the
// policy — attached to the user — and a security group in each region.
func TestSetup_CreatesWhatIsMissing(t *testing.T) {
	iam := bare()
	ec2 := map[string]*fakeEC2{"eu-north-1": {offered: []string{"eu-north-1b", "eu-north-1a"}}, "eu-south-2": {offered: []string{"eu-south-2a"}}}
	quotas := quotasOf(8, 8)
	var log lines
	s := setupOf(iam, ec2, quotas, &log)
	s.User, s.KeyFile, s.Account = "acme-translate", filepath.Join(t.TempDir(), "aws.env"), account
	require.NoError(t, s.Converge(context.Background()))

	trust := `{"Statement":[{"Action":"sts:AssumeRole","Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"}}],"Version":"2012-10-17"}`
	assert.Equal(t, []string{
		"CreateUser acme-translate",
		"CreateAccessKey acme-translate",
		"CreateRole acme-translate-instance " + trust,
		"AttachRolePolicy acme-translate-instance arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore",
		"CreateInstanceProfile acme-translate-instance",
		"AddRoleToInstanceProfile acme-translate-instance acme-translate-instance",
		"CreateServiceLinkedRole spot.amazonaws.com",
		"CreatePolicy acme-translate-operator",
		"AttachUserPolicy acme-translate " + policyARN,
	}, iam.calls)
	assert.JSONEq(t, operatorPolicy(account, "acme-translate", regions, []string{"g7e.2xlarge"}).document(), iam.document)

	key, err := os.ReadFile(s.KeyFile)
	require.NoError(t, err)
	assert.Equal(t, "AWS_ACCESS_KEY_ID=AKIAEXAMPLE\nAWS_SECRET_ACCESS_KEY=secret/example\n", string(key))
	info, err := os.Stat(s.KeyFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	assert.Equal(t, []string{"acme-translate"}, ec2["eu-north-1"].createdGroups)
	assert.Equal(t, []string{"acme-translate"}, ec2["eu-south-2"].createdGroups)
	assert.Contains(t, log.get(), "eu-north-1: g7e.2xlarge in eu-north-1a, eu-north-1b; G and VT quotas spot 8 vCPU, on-demand 8 vCPU")
	assert.Contains(t, log.get(), "user acme-translate created, its key written to "+s.KeyFile)
	assert.Empty(t, quotas.requested)
}

// An account already converged is left as it is, the user's policy attached
// again, which IAM takes as a no-op; a quota short of one instance is
// requested.
func TestSetup_UnchangedIsLeftAlone(t *testing.T) {
	iam := converged()
	ec2 := map[string]*fakeEC2{"eu-north-1": {groups: []string{"sg-1"}, offered: []string{"eu-north-1a"}}, "eu-south-2": {groups: []string{"sg-2"}}}
	quotas := quotasOf(8, 0)
	var log lines
	s := setupOf(iam, ec2, quotas, &log)
	s.User, s.KeyFile = "acme-translate", filepath.Join(t.TempDir(), "aws.env")
	require.NoError(t, s.Converge(context.Background()))
	assert.Equal(t, []string{"AttachUserPolicy acme-translate " + policyARN}, iam.calls)
	assert.NoFileExists(t, s.KeyFile, "a key is written only for a user created")
	assert.Empty(t, ec2["eu-north-1"].createdGroups)
	assert.Contains(t, log.get(), "eu-north-1: g7e.2xlarge in eu-north-1a; G and VT quotas spot 8 vCPU, on-demand 0 vCPU (8 requested now)")
	assert.Contains(t, log.get(), "WARNING eu-south-2: g7e.2xlarge offered in no zone — a run finds nothing to launch there")
	assert.Equal(t, []string{"L-DB2E81BA=8"}, quotas.requested)

	// Without a user, none is managed.
	iam = converged()
	require.NoError(t, setupOf(iam, ec2, quotasOf(8, 8), &lines{}).Converge(context.Background()))
	assert.Empty(t, iam.calls)
}

// A quota whose request is still open is not requested again.
func TestSetup_AQuotaRequestStillOpenIsNotRepeated(t *testing.T) {
	ec2 := map[string]*fakeEC2{"eu-north-1": {groups: []string{"sg-1"}, offered: []string{"eu-north-1a"}}, "eu-south-2": {groups: []string{"sg-2"}}}
	quotas := quotasOf(0, 0)
	quotas.open = map[string]sqtypes.RequestStatus{"L-3819A6DF": sqtypes.RequestStatusPending, "L-DB2E81BA": sqtypes.RequestStatusCaseOpened}
	var log lines
	require.NoError(t, setupOf(converged(), ec2, quotas, &log).Converge(context.Background()))
	assert.Contains(t, log.get(), "eu-north-1: g7e.2xlarge in eu-north-1a; G and VT quotas spot 0 vCPU (8 requested, PENDING), on-demand 0 vCPU (8 requested, CASE_OPENED)")
	assert.Empty(t, quotas.requested)
}

// Named no region, setup prepares every region the account has enabled that
// offers an instance type, looked at from the home region; one that cannot be
// reached is left out with a warning, and setup succeeds with the rest.
func TestSetup_EveryRegionOfferingTheTypes(t *testing.T) {
	iam := bare()
	ec2 := map[string]*fakeEC2{
		homeRegion:   {regions: []string{"eu-south-2", "us-east-1", "me-south-1", "eu-north-1"}},
		"eu-north-1": {offered: []string{"eu-north-1a"}},
		"eu-south-2": {offered: []string{"eu-south-2a"}},
		"me-south-1": {unreachable: errors.New("connect timeout")},
	}
	var log lines
	s := setupOf(iam, ec2, quotasOf(8, 8), &log)
	s.regions = nil
	require.NoError(t, s.Converge(context.Background()))
	assert.Contains(t, log.get(), "WARNING me-south-1: not reached, so not prepared — run setup again to have it, should it offer a type: connect timeout")
	assert.JSONEq(t, operatorPolicy(account, "acme-translate", []string{"eu-north-1", "eu-south-2"}, []string{"g7e.2xlarge"}).document(), iam.document)
	assert.Equal(t, []string{"acme-translate"}, ec2["eu-north-1"].createdGroups)
	assert.Equal(t, []string{"acme-translate"}, ec2["eu-south-2"].createdGroups)
	assert.Empty(t, ec2[homeRegion].createdGroups, "offering no type, not prepared")
	assert.Contains(t, log.get(), "translation GPU: AWS setup of acme-translate in account 123456789012, in eu-north-1, eu-south-2")

	// An interrupt while the regions are looked at ends setup as the
	// interrupt it is, warning of no region and preparing none.
	iam = bare()
	var interrupted lines
	s = setupOf(iam, ec2, quotasOf(8, 8), &interrupted)
	s.regions = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, s.Converge(ctx), context.Canceled)
	assert.Empty(t, interrupted.get())
	assert.Empty(t, iam.calls)

	// With no region offering a type, there is nothing to prepare.
	iam = bare()
	s = setupOf(iam, map[string]*fakeEC2{homeRegion: {regions: []string{homeRegion}}}, quotasOf(8, 8), &lines{})
	s.regions = nil
	assert.EqualError(t, s.Converge(context.Background()), "no region the account has enabled offers g7e.2xlarge")
	assert.Empty(t, iam.calls)
}

// A policy that drifted — a region or an instance type added — gets a new
// default version.
func TestSetup_DriftedPolicyGetsANewVersion(t *testing.T) {
	iam := converged()
	iam.document = url.PathEscape(operatorPolicy(account, "acme-translate", regions[:1], []string{"g7e.2xlarge"}).document())
	ec2 := map[string]*fakeEC2{"eu-north-1": {groups: []string{"sg-1"}}, "eu-south-2": {groups: []string{"sg-2"}}}
	var log lines
	require.NoError(t, setupOf(iam, ec2, quotasOf(8, 8), &log).Converge(context.Background()))
	assert.Equal(t, []string{"CreatePolicyVersion default=true"}, iam.calls)
	assert.JSONEq(t, operatorPolicy(account, "acme-translate", regions, []string{"g7e.2xlarge"}).document(), iam.document)
	assert.Contains(t, log.get(), "policy acme-translate-operator is now v2")
}

// IAM keeps five versions of a policy: the oldest that is not the default
// makes room for the new one.
func TestSetup_FiveVersionsRotate(t *testing.T) {
	iam := converged()
	iam.document = "%7B%7D"
	iam.versions = []iamtypes.PolicyVersion{
		{VersionId: awssdk.String("v3"), CreateDate: awssdk.Time(time.Unix(3, 0))},
		{VersionId: awssdk.String("v5"), IsDefaultVersion: true, CreateDate: awssdk.Time(time.Unix(5, 0))},
		{VersionId: awssdk.String("v2"), CreateDate: awssdk.Time(time.Unix(2, 0))},
		{VersionId: awssdk.String("v4"), CreateDate: awssdk.Time(time.Unix(4, 0))},
		{VersionId: awssdk.String("v1"), CreateDate: awssdk.Time(time.Unix(1, 0))},
	}
	ec2 := map[string]*fakeEC2{"eu-north-1": {groups: []string{"sg-1"}}, "eu-south-2": {groups: []string{"sg-2"}}}
	require.NoError(t, setupOf(iam, ec2, quotasOf(8, 8), &lines{}).Converge(context.Background()))
	assert.Equal(t, []string{"DeletePolicyVersion v1", "CreatePolicyVersion default=true"}, iam.calls)
}

// A region named wrong would be written into the policy: it is refused before
// anything is done.
func TestSetup_RefusesARegionNamedWrong(t *testing.T) {
	iam := bare()
	s := setupOf(iam, nil, quotasOf(8, 8), &lines{})
	s.regions = []string{"eu-north-1", " eu-south-2"}
	assert.EqualError(t, s.Converge(context.Background()), `" eu-south-2" is no AWS region`)
	assert.Empty(t, iam.calls)
}

// A setup is made with what it needs, its account, user and key file left to
// set where wanted; its account-wide clients in the home region.
func TestNewSetup(t *testing.T) {
	var log lines
	s := NewSetup(awssdk.Config{}, "acme", regions, []string{"g7e.2xlarge"}, log.log)
	assert.Equal(t, Names("acme"), s.names)
	assert.Equal(t, regions, s.regions)
	assert.Equal(t, []string{"g7e.2xlarge"}, s.types)
	assert.Equal(t, "eu-south-2", s.regional("eu-south-2").ec2.(*ec2.Client).Options().Region)
	assert.Equal(t, homeRegion, s.iam.(*iam.Client).Options().Region)
	assert.Equal(t, homeRegion, s.sts.(*sts.Client).Options().Region)
	s.log("said")
	assert.Equal(t, []string{"said"}, log.get())
}

// Setup acts in the account it is told to and no other.
func TestSetup_RefusesAnotherAccount(t *testing.T) {
	iam := bare()
	s := setupOf(iam, nil, quotasOf(8, 8), &lines{})
	s.Account = "999999999999"
	assert.EqualError(t, s.Converge(context.Background()), "these credentials are account 123456789012, not 999999999999")
	assert.Empty(t, iam.calls)
}

// The policy admits the profiles' instance types, tagged with the
// deployment's name, and confines everything regional — but reading its own
// policy, passing the instance role and the Pricing API — to its regions,
// which a run reads back; what IAM returns of it — URL-encoded — is compared
// as the document it is.
func TestOperatorPolicy(t *testing.T) {
	p := operatorPolicy(account, "acme", []string{"eu-north-1", "us-west-2"}, []string{"g6e.xlarge", "g7e.2xlarge"})
	document := p.document()
	for _, want := range []string{
		`"Sid":"OnlyInThePreparedRegions","Effect":"Deny","NotAction":["iam:GetPolicy","iam:GetPolicyVersion","iam:PassRole","pricing:GetProducts"],"Resource":"*","Condition":{"StringNotEquals":{"aws:RequestedRegion":["eu-north-1","us-west-2"]}}`,
		`"Sid":"LaunchOnlyTheTranslationInstance","Effect":"Allow","Action":"ec2:RunInstances","Resource":"arn:aws:ec2:*:*:instance/*","Condition":{"StringEquals":{"aws:RequestTag/Project":"acme","ec2:InstanceType":["g6e.xlarge","g7e.2xlarge"]}}`,
		`"Resource":["arn:aws:ec2:*::image/*","arn:aws:ec2:*:*:subnet/*"`,
		`"Sid":"PassTheInstanceRole","Effect":"Allow","Action":"iam:PassRole","Resource":"arn:aws:iam::123456789012:role/acme-instance"`,
		`"Sid":"ReadItsOwnPolicy","Effect":"Allow","Action":["iam:GetPolicy","iam:GetPolicyVersion"],"Resource":"arn:aws:iam::123456789012:policy/acme-operator"`,
		`"Sid":"TunnelIntoOwnInstances","Effect":"Allow","Action":"ssm:StartSession","Resource":"arn:aws:ec2:*:*:instance/*","Condition":{"StringEquals":{"ssm:resourceTag/Project":"acme"}}`,
		`"arn:aws:ssm:*:*:session/${aws:username}-*","arn:aws:ssm:*:*:session/${aws:userid}-*"`,
	} {
		assert.Contains(t, document, want)
	}
	assert.Equal(t, 12, strings.Count(document, `"Effect":"Allow"`))
	assert.Equal(t, 1, strings.Count(document, `"Effect":"Deny"`))

	same, err := p.sameDocument(url.PathEscape(document))
	require.NoError(t, err)
	assert.True(t, same)
	same, err = p.sameDocument(url.PathEscape(strings.Replace(document, "acme", "other", 1)))
	require.NoError(t, err)
	assert.False(t, same)
	_, err = p.sameDocument("%zz")
	assert.ErrorContains(t, err, "decode the policy document")
	_, err = p.sameDocument("not json")
	assert.ErrorContains(t, err, "parse the policy document")

	in, err := regionsIn(url.PathEscape(document))
	require.NoError(t, err)
	assert.Equal(t, []string{"eu-north-1", "us-west-2"}, in)
	_, err = regionsIn("%zz")
	assert.ErrorContains(t, err, "decode the policy document")
	_, err = regionsIn(url.PathEscape(strings.Replace(document, `["eu-north-1","us-west-2"]`, `[1]`, 1)))
	assert.EqualError(t, err, "the operator policy's OnlyInThePreparedRegions statement lists no regions")

	names := Names("acme")
	assert.Equal(t, []string{"acme-operator", "acme-instance", "acme", "acme"}, []string{names.Policy(), names.InstanceProfile(), names.SecurityGroup(), names.Tag()})
	assert.Equal(t, "arn:aws:iam::123456789012:policy/acme-operator", names.PolicyARN(account))
}
