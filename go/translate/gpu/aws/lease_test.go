package aws

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// launched is where a lease's launch put its instance.
func launched(l *Lease) (placed, error) {
	offers, err := l.offers(context.Background())
	if err != nil {
		return placed{}, err
	}
	return l.launch(context.Background(), offers)
}

// A launch tries every zone of every prepared region, spot and on demand,
// cheapest first, moving on from one that has no capacity — whichever region
// it is in. The instance carries the tag, the role, IMDSv2 and a shutdown that
// terminates, and boots the profile's script.
func TestLaunch_CheapestFirstAcrossRegionsAndCapacityFallback(t *testing.T) {
	north, west := region(), west()
	west.refusing = map[string]string{"us-west-2a": noCapacity}
	north.refusing = map[string]string{"eu-north-1b": noCapacity}
	north.offered = append(north.offered, "eu-north-1d") // offered, but with no default subnet
	north.subnets["eu-north-1e"] = "subnet-e"            // a subnet in a zone not offering the type
	north.spotPrices["eu-north-1e"] = "0.100000"
	var log lines
	instance, err := launched(lease(map[string]*fakeEC2{"eu-north-1": north, "us-west-2": west}, &fakeTunnels{}, &log))
	require.NoError(t, err)
	assert.Equal(t, "i-us-west-2b", instance.id)
	assert.Equal(t, offer{region: "us-west-2", zone: "us-west-2b", subnet: "subnet-wb", group: "sg-w", market: spot, price: 0.7}, instance.offer)
	assert.Equal(t, []string{"eu-north-1b"}, north.launchedZones(), "0.611 between us-west-2a's 0.5 and us-west-2b's 0.7")
	assert.Equal(t, []string{"us-west-2a", "us-west-2b"}, west.launchedZones())

	in := west.launched[1]
	assert.Equal(t, "ami-dlami", awssdk.ToString(in.ImageId))
	assert.Equal(t, ec2types.InstanceType("g7e.2xlarge"), in.InstanceType)
	assert.Equal(t, int32(1), awssdk.ToInt32(in.MinCount))
	assert.Equal(t, int32(1), awssdk.ToInt32(in.MaxCount))
	assert.Equal(t, []string{"sg-w"}, in.SecurityGroupIds)
	assert.Equal(t, "acme-translate-instance", awssdk.ToString(in.IamInstanceProfile.Name))
	assert.Equal(t, ec2types.HttpTokensStateRequired, in.MetadataOptions.HttpTokens)
	assert.Equal(t, ec2types.ShutdownBehaviorTerminate, in.InstanceInitiatedShutdownBehavior)
	assert.Equal(t, ec2types.MarketTypeSpot, in.InstanceMarketOptions.MarketType)
	assert.Equal(t, ec2types.InstanceInterruptionBehaviorTerminate, in.InstanceMarketOptions.SpotOptions.InstanceInterruptionBehavior)
	require.Len(t, in.TagSpecifications, 2)
	for i, kind := range []ec2types.ResourceType{ec2types.ResourceTypeInstance, ec2types.ResourceTypeVolume} {
		assert.Equal(t, kind, in.TagSpecifications[i].ResourceType)
		assert.Equal(t, []ec2types.Tag{{Key: awssdk.String("Project"), Value: awssdk.String("acme-translate")}}, in.TagSpecifications[i].Tags)
	}
	userData, err := base64.StdEncoding.DecodeString(awssdk.ToString(in.UserData))
	require.NoError(t, err)
	assert.Equal(t, builtins["gemma4-31b"].userData(), string(userData))
	all := strings.Join(log.get(), "\n")
	assert.Contains(t, all, "10 place(s) to launch a g7e.2xlarge in 2 region(s); cheapest spot in us-west-2a at $0.500/h")
	assert.Contains(t, all, "spot us-west-2a: api error InsufficientInstanceCapacity")
	assert.Contains(t, all, "spot eu-north-1b: api error InsufficientInstanceCapacity")
	assert.Contains(t, log.get(), "i-us-west-2b: g7e.2xlarge spot in us-west-2b at $0.700/h, ami-dlami")
}

// On demand, a zone is priced as the Pricing API prices its region, and is
// launched in with no market options once it is the cheapest left.
func TestLaunch_OnDemandWhereSpotIsRefused(t *testing.T) {
	north, west := region(), west()
	north.refusing = map[string]string{"eu-north-1a": noCapacity, "eu-north-1b": noCapacity, "eu-north-1c": noCapacity}
	west.refusing = map[string]string{"us-west-2a": noCapacity}
	var log lines
	l := leaseWith(map[string]*fakeEC2{"eu-north-1": north, "us-west-2": west},
		map[string]*fakeQuotas{"eu-north-1": quotasOf(8, 8), "us-west-2": quotasOf(0, 8)}, &fakeTunnels{}, &log)
	instance, err := launched(l)
	require.NoError(t, err)
	assert.Equal(t, offer{region: "us-west-2", zone: "us-west-2b", subnet: "subnet-wb", group: "sg-w", market: onDemand, price: 2.7183}, instance.offer)
	assert.Equal(t, []string{"us-west-2a", "us-west-2b"}, west.launchedZones(), "on demand at 2.72 before eu-north-1's 3.14")
	assert.Nil(t, west.launched[1].InstanceMarketOptions)
	assert.Contains(t, log.get(), "spot quota short of one g7e.2xlarge in us-west-2")
	assert.Contains(t, log.get(), "i-us-west-2b: g7e.2xlarge on-demand in us-west-2b at $2.718/h, ami-dlami")
}

// A market whose quota cannot hold one instance offers nothing; with nothing
// offered anywhere the lease says so, pointing at what it said of each.
func TestOffers_OnlyWhereTheQuotaHoldsOne(t *testing.T) {
	var log lines
	l := leaseWith(north(region()), map[string]*fakeQuotas{"eu-north-1": quotasOf(4, 0)}, &fakeTunnels{}, &log)
	_, err := l.offers(context.Background())
	assert.EqualError(t, err, "nowhere to launch a g7e.2xlarge now: of the 1 region(s) setup prepared, 0 could not be read, and none of the others offers it with its price read and the quota for one — each is said above")
	assert.Contains(t, log.get(), "spot quota short of one g7e.2xlarge in eu-north-1")
	assert.Contains(t, log.get(), "on-demand quota short of one g7e.2xlarge in eu-north-1")
}

// An interrupt while the regions are read ends the lease as the interrupt it
// is, not as a region passed over.
func TestOffers_InterruptedWhileReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var log lines
	_, err := lease(north(region()), &fakeTunnels{}, &log).offers(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, strings.Join(log.get(), "\n"), "passed over")
}

// The regions are those the operator policy confines the operator to; with no
// policy, or one setup did not write, the lease is refused.
func TestOffers_TheRegionsTheOperatorPolicyNames(t *testing.T) {
	unprepared := west()
	unprepared.unreachable = errors.New("read")
	var log lines
	l := lease(map[string]*fakeEC2{"eu-north-1": region(), "us-west-2": unprepared}, &fakeTunnels{}, &log)
	l.iam.(*fakeIAM).document = url.PathEscape(operatorPolicy(account, "acme-translate", []string{"eu-north-1"}, []string{"g7e.2xlarge"}).document())
	offers, err := l.offers(context.Background())
	require.NoError(t, err)
	assert.Len(t, offers, 6, "three zones, spot and on demand")
	assert.NotContains(t, strings.Join(log.get(), "\n"), "us-west-2", "a region the policy does not name is not read")

	l.iam.(*fakeIAM).policy = ""
	_, err = l.offers(context.Background())
	assert.EqualError(t, err, "no operator policy arn:aws:iam::123456789012:policy/acme-translate-operator: `translate-gpu setup` prepares the account")

	l = lease(north(region()), &fakeTunnels{}, &lines{})
	l.iam.(*fakeIAM).document = url.PathEscape(`{"Version":"2012-10-17","Statement":[]}`)
	_, err = l.offers(context.Background())
	assert.EqualError(t, err, "the operator policy has no OnlyInThePreparedRegions statement: run `translate-gpu setup` again")
}

// A region that cannot be read — unreachable, its security group gone — is
// said and passed over, and the lease names how many were.
func TestOffers_ARegionNotReadIsPassedOver(t *testing.T) {
	gone, bare := west(), region()
	gone.unreachable = errors.New("connect timeout")
	var log lines
	l := lease(map[string]*fakeEC2{"eu-north-1": region(), "us-west-2": gone, "eu-south-2": bare}, &fakeTunnels{}, &log)
	bare.groups = nil
	offers, err := l.offers(context.Background())
	require.NoError(t, err)
	assert.Len(t, offers, 6)
	assert.Contains(t, log.get(), "us-west-2: passed over, not read: connect timeout")
	assert.Contains(t, log.get(), "eu-south-2: passed over, not read: no security group acme-translate: `translate-gpu setup` prepares a region")

	_, err = lease(map[string]*fakeEC2{"us-west-2": gone}, &fakeTunnels{}, &lines{}).offers(context.Background())
	assert.EqualError(t, err, "nowhere to launch a g7e.2xlarge now: of the 1 region(s) setup prepared, 1 could not be read, and none of the others offers it with its price read and the quota for one — each is said above")
}

// A market whose price cannot be read — the Pricing API listing none, or not
// one — is said and passed over in that region; its other market stands.
func TestOffers_AMarketNotPricedIsPassedOver(t *testing.T) {
	var log lines
	l := lease(north(region()), &fakeTunnels{}, &log)
	l.pricing = fakePricing{"eu-north-1": `{"terms":{"OnDemand":{}}}`}
	offers, err := l.offers(context.Background())
	require.NoError(t, err)
	assert.Len(t, offers, 3, "spot in each zone")
	for _, o := range offers {
		assert.Equal(t, spot, o.market)
	}
	assert.Contains(t, log.get(), "eu-north-1: on-demand passed over, its price not read: the Pricing API's product for g7e.2xlarge in eu-north-1 has no single on-demand price")

	l.pricing = fakePricing{}
	offers, err = l.offers(context.Background())
	require.NoError(t, err)
	assert.Len(t, offers, 3)
	assert.Contains(t, log.get(), "eu-north-1: on-demand passed over, its price not read: the Pricing API lists 0 on-demand products for g7e.2xlarge in eu-north-1, not one")
}

// An instance of the deployment still running in any prepared region refuses
// the lease: another lease's, or one whose launcher died.
func TestOffers_AnotherInstanceRunningRefuses(t *testing.T) {
	west := west()
	west.previous = []ec2types.Instance{{InstanceId: awssdk.String("i-old"), State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning}}}
	_, err := lease(map[string]*fakeEC2{"eu-north-1": region(), "us-west-2": west}, &fakeTunnels{}, &lines{}).offers(context.Background())
	assert.EqualError(t, err, "i-old is running in us-west-2: another lease's, or one whose launcher died — that one ends itself at its idle limit")
}

// With no zone's capacity to give, spot or on demand, the lease says so.
func TestLaunch_NoCapacityAnywhere(t *testing.T) {
	ec2 := region()
	ec2.refusing = map[string]string{"eu-north-1a": noCapacity, "eu-north-1b": noCapacity, "eu-north-1c": noCapacity}
	_, err := launched(lease(north(ec2), &fakeTunnels{}, &lines{}))
	assert.EqualError(t, err, "nowhere would launch a g7e.2xlarge now, spot or on demand — each refusal is said above; run again later")
	assert.Len(t, ec2.launched, 6)
}

// A zone not giving the type on a market is moved on from, as one with no
// capacity is; a region's quota on a market spent by other instances passes
// over every zone of it on that market, and another error ends the lease.
func TestLaunch_RefusalsMovedOnFrom(t *testing.T) {
	north, west := region(), west()
	west.refusing = map[string]string{"us-west-2a": "MaxSpotInstanceCountExceeded"}
	north.refusing = map[string]string{"eu-north-1b": "Unsupported"}
	var log lines
	instance, err := launched(lease(map[string]*fakeEC2{"eu-north-1": north, "us-west-2": west}, &fakeTunnels{}, &log))
	require.NoError(t, err)
	assert.Equal(t, "i-eu-north-1c", instance.id)
	assert.Equal(t, []string{"us-west-2a"}, west.launchedZones(), "us-west-2b's spot passed over with its region's spent quota")
	assert.Equal(t, []string{"eu-north-1b", "eu-north-1c"}, north.launchedZones())
	assert.Contains(t, log.get(), "spot in us-west-2: api error MaxSpotInstanceCountExceeded: refused in us-west-2a — its quota spent by other instances, its other zones are passed over")
	assert.Contains(t, log.get(), "spot eu-north-1b: api error Unsupported: refused in eu-north-1b")

	ec2 := region()
	ec2.refusing = map[string]string{"eu-north-1b": "InvalidParameterValue"}
	_, err = launched(lease(map[string]*fakeEC2{"eu-north-1": ec2}, &fakeTunnels{}, &lines{}))
	assert.EqualError(t, err, "eu-north-1b: api error InvalidParameterValue: refused in eu-north-1b")
}

// A spot request of an earlier lease holds the region's quota for minutes
// after its instance is gone: the launch waits for EC2 to release it, once per
// region. An earlier instance found running by then is refused.
func TestLaunch_WaitsOutTheSpotQuota(t *testing.T) {
	ec2 := region()
	ec2.previous = []ec2types.Instance{{InstanceId: awssdk.String("i-old"), SpotInstanceRequestId: awssdk.String("sir-1"), State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameTerminated}}}
	ec2.openRequests = [][]string{{"sir-1"}, {"sir-1"}, {}}
	ec2.refusing = map[string]string{"eu-north-1b": noCapacity}
	var log lines
	instance, err := launched(lease(north(ec2), &fakeTunnels{}, &log))
	require.NoError(t, err)
	assert.Equal(t, "i-eu-north-1c", instance.id)
	assert.Equal(t, 1, count(log.get(), "waiting for EC2 to release the spot quota i-old held in eu-north-1; it takes minutes after termination"), "said once, however long it waits")
	assert.Empty(t, ec2.openRequests, "looked at until released, and not again for the next zone")

	l := lease(north(ec2), &fakeTunnels{}, &lines{})
	offers, err := l.offers(context.Background())
	require.NoError(t, err)
	ec2.previous[0].State.Name = ec2types.InstanceStateNameRunning
	_, err = l.launch(context.Background(), offers)
	assert.EqualError(t, err, "i-old is running in eu-north-1: another lease's, or one whose launcher died — that one ends itself at its idle limit")

	ec2.previous[0].State.Name = ec2types.InstanceStateNameTerminated
	ec2.openRequests = [][]string{{"sir-1"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = l.launch(ctx, offers)
	assert.ErrorIs(t, err, context.Canceled, "the wait ends with the lease")
}

// A lease runs its job once vLLM serves, over a tunnel in the instance's
// region, and terminates the instance — skipping the OS's shutdown — when the
// job is done, saying what it cost. The boot script's console lines are
// relayed as they come, each once.
func TestLease_TerminatesWhenTheJobIsDone(t *testing.T) {
	ec2 := region()
	ec2.consoles = []string{
		"[   0.1] kernel\ntranslate-gpu| watchdog armed\n",
		"[   0.1] kernel\ntranslate-gpu| watchdog armed\ntranslate-gpu| pulling vllm\ntranslate-gpu| half a li",
	}
	tunnels := &fakeTunnels{server: healthyAfter(t, 1)}
	var log lines
	var jobs []string
	err := lease(north(ec2), tunnels, &log).Run(context.Background(), func(_ context.Context, baseURL string) error {
		jobs = append(jobs, baseURL)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{tunnels.server.URL}, jobs)
	assert.Equal(t, []string{"eu-north-1"}, tunnels.regions)
	assert.Equal(t, []string{"i-eu-north-1b"}, ec2.terminatedInstances())
	assert.True(t, tunnels.latest().wasClosed)
	all := log.get()
	assert.Equal(t, 1, count(all, "   | watchdog armed"))
	assert.Equal(t, 1, count(all, "   | pulling vllm"))
	assert.Zero(t, count(all, "   | half a li"), "a line still being written waits")
	assert.Contains(t, all, "i-eu-north-1b: pending")
	assert.Contains(t, all, "i-eu-north-1b: running")
	assert.Contains(t, strings.Join(all, "\n"), "i-eu-north-1b shutting-down, billing stopped; it ran 0 min: ~$0.00 at $0.611/h")
}

// While a job runs, /health is looked at through the tunnel every poll — input
// that keeps Session Manager from ending a session an answer leaves silent.
func TestLease_TunnelKeptAliveWhileTheJobRuns(t *testing.T) {
	var looks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			looks.Add(1)
		}
	}))
	defer server.Close()
	err := lease(north(region()), &fakeTunnels{server: server}, &lines{}).Run(context.Background(), func(context.Context, string) error {
		before := looks.Load()
		require.Eventually(t, func() bool { return looks.Load() >= before+3 }, 5*time.Second, time.Millisecond)
		return nil
	})
	require.NoError(t, err)
}

// A job that fails with the tunnel open and the instance running ends the
// lease, and the instance with it.
func TestLease_TerminatesWhenTheJobFails(t *testing.T) {
	ec2 := region()
	err := lease(north(ec2), &fakeTunnels{server: healthy(t)}, &lines{}).Run(context.Background(), func(context.Context, string) error {
		return errors.New("2 failure(s)")
	})
	assert.EqualError(t, err, "2 failure(s)")
	assert.Equal(t, []string{"i-eu-north-1b"}, ec2.terminatedInstances())
}

// An interrupt — the context ended — ends the lease wherever it is, and the
// instance is terminated all the same.
func TestLease_TerminatesWhenInterrupted(t *testing.T) {
	ec2 := region()
	ctx, cancel := context.WithCancel(context.Background())
	err := lease(north(ec2), &fakeTunnels{server: healthy(t)}, &lines{}).Run(ctx, func(ctx context.Context, _ string) error {
		cancel()
		return ctx.Err()
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{"i-eu-north-1b"}, ec2.terminatedInstances())

	// Before vLLM ever serves, too.
	loading := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer loading.Close()
	ec2 = region()
	ctx, cancel = context.WithCancel(context.Background())
	var looks atomic.Int32
	var log lines
	l := lease(north(ec2), &fakeTunnels{server: loading}, &log)
	l.log = func(line string) {
		log.log(line)
		if strings.HasPrefix(line, "SSM agent online") && looks.Add(1) == 1 {
			cancel()
		}
	}
	err = l.Run(ctx, func(context.Context, string) error { t.Fatal("vLLM never served"); return nil })
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{"i-eu-north-1b"}, ec2.terminatedInstances())
}

// A job that fails because the tunnel closed runs again over a new one.
func TestLease_ClosedTunnelRerunsTheJob(t *testing.T) {
	ec2 := region()
	tunnels := &fakeTunnels{server: healthy(t)}
	var log lines
	jobs := 0
	err := lease(north(ec2), tunnels, &log).Run(context.Background(), func(context.Context, string) error {
		jobs++
		if jobs == 1 {
			tunnels.latest().drop()
			return errors.New("connection refused")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, jobs)
	assert.Len(t, tunnels.opened, 2)
	assert.Contains(t, log.get(), "the run stopped with the tunnel (connection refused); running it again")
	assert.Contains(t, log.get(), "the tunnel closed: Cannot perform start session: EOF")
	assert.Equal(t, []string{"i-eu-north-1b"}, ec2.terminatedInstances())
}

// An agent online may not take a session yet: a tunnel that does not open is
// opened on the next pass, and the boot the instance has done is not lost.
func TestLease_TunnelThatWillNotOpenIsOpenedAgain(t *testing.T) {
	ec2 := region()
	tunnels := &fakeTunnels{server: healthy(t), refusals: 2}
	var log lines
	jobs := 0
	err := lease(north(ec2), tunnels, &log).Run(context.Background(), func(context.Context, string) error {
		jobs++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, jobs)
	assert.Equal(t, 2, count(log.get(), "the tunnel did not open: api error TargetNotConnected: i-eu-north-1b is not connected."))
	assert.Equal(t, []string{"i-eu-north-1b"}, ec2.terminatedInstances())
}

// A lease is made whole: everything it needs given at once, its account-wide
// clients in the home region, its regional ones in each region asked for.
func TestNewLease(t *testing.T) {
	var log lines
	l := NewLease(awssdk.Config{}, "acme", builtins["gemma4-26b"], log.log)
	assert.Equal(t, Names("acme"), l.names)
	assert.Equal(t, builtins["gemma4-26b"], l.profile)
	l.log("said")
	assert.Equal(t, []string{"said"}, log.get())
	assert.Equal(t, homeRegion, l.iam.(*iam.Client).Options().Region)
	assert.Equal(t, homeRegion, l.sts.(*sts.Client).Options().Region)
	assert.Equal(t, homeRegion, l.pricing.(*pricing.Client).Options().Region)
	assert.Equal(t, "us-west-2", l.regional("us-west-2").ec2.(*ec2.Client).Options().Region)
	assert.Equal(t, "us-west-2", l.tunnels.(*ssmTunnels).ssm("us-west-2").(*ssm.Client).Options().Region)
	assert.Equal(t, pollInterval, l.poll)
}

// The instance leaving the running state — a spot reclaim, its own deadline —
// ends the lease, saying why and that running again resumes.
func TestLease_InstanceLeavingRunningEndsIt(t *testing.T) {
	ec2 := region()
	ec2.states = []ec2types.InstanceStateName{ec2types.InstanceStateNameRunning, ec2types.InstanceStateNameShuttingDown}
	ec2.stateReason = "Server.SpotInstanceTermination: Spot instance termination"
	never := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer never.Close()
	err := lease(north(ec2), &fakeTunnels{server: never}, &lines{}).Run(context.Background(), func(context.Context, string) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "i-eu-north-1b is shutting-down: Server.SpotInstanceTermination: Spot instance termination")
	assert.Contains(t, err.Error(), "`aws ec2 get-console-output --region eu-north-1 --latest --instance-id i-eu-north-1b`")
	assert.Contains(t, err.Error(), "Run the same command again: it resumes")
	assert.Equal(t, []string{"i-eu-north-1b"}, ec2.terminatedInstances())

	// Gone while the job ran, the job's failure is the instance's.
	ec2 = region()
	ec2.states = []ec2types.InstanceStateName{ec2types.InstanceStateNameRunning, ec2types.InstanceStateNameShuttingDown}
	err = lease(north(ec2), &fakeTunnels{server: healthy(t)}, &lines{}).Run(context.Background(), func(context.Context, string) error { return errors.New("timeout") })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "i-eu-north-1b is shutting-down: no reason given")
}

// What the tunnel needs on this machine is refused before anything launches.
func TestLease_RefusedBeforeLaunchWithoutThePlugin(t *testing.T) {
	ec2 := region()
	err := lease(north(ec2), &fakeTunnels{notReady: errors.New("session-manager-plugin is not installed")}, &lines{}).Run(context.Background(), func(context.Context, string) error { return nil })
	assert.EqualError(t, err, "session-manager-plugin is not installed")
	assert.Empty(t, ec2.launched)
	assert.Empty(t, ec2.terminatedInstances())
}

func count(lines []string, line string) int {
	n := 0
	for _, l := range lines {
		if l == line {
			n++
		}
	}
	return n
}
