package aws

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

const (
	// dlami is the parameter naming the current Deep Learning AMI.
	dlami = "/aws/service/deeplearning/ami/x86_64/base-oss-nvidia-driver-gpu-ubuntu-24.04/latest/ami-id"
	// vllmPort is where vLLM listens on the instance's loopback (instance.sh).
	vllmPort = 8000
	// consoleTag starts every console line the boot script writes.
	consoleTag = "translate-gpu| "
	// pollInterval is how often the instance, its agent and vLLM are looked
	// at: AWS tells nothing about them unasked.
	pollInterval = 10 * time.Second
	// healthTimeout bounds one look at vLLM's /health.
	healthTimeout = 5 * time.Second
)

// refusal is how far a launch's refusal reaches: its zone, or every zone of
// its region on its market.
type refusal int

const (
	zoneRefused refusal = iota + 1
	marketRefused
)

// refusals are the errors a launch moves on from: a zone with no instance of
// the type to give, or not giving it on that market; and a region's quota on a
// market that other instances have spent.
var refusals = map[string]refusal{
	"InsufficientInstanceCapacity": zoneRefused,
	"Unsupported":                  zoneRefused,
	"VcpuLimitExceeded":            marketRefused,
	"MaxSpotInstanceCountExceeded": marketRefused,
}

// Job is what a lease is taken for: run against the model served at baseURL.
type Job func(ctx context.Context, baseURL string) error

// Lease rents one instance for the length of a job, wherever it costs least
// at the time: of the regions setup prepared — those the operator policy
// confines the operator to — every zone offering the profile's instance type,
// spot at its price now where the region's spot quota holds one instance and
// on demand at the Pricing API's where its on-demand quota does, tried
// cheapest first, moving on from one with no capacity. The instance serves the
// profile's model through vLLM, reached over an SSM tunnel. A tunnel that will
// not open, or that closes, is opened again, and a job that fails because the
// tunnel closed runs again over a new one; any other failure ends the lease.
// The instance is terminated on every way out — the job done, an error, the
// context ended — and keeps deadlines of its own besides (instance.sh), so a
// launcher that dies mid-run leaves nothing billing; its leaving the running
// state ends the lease.
type Lease struct {
	names   Names
	profile Profile
	log     func(string)

	global
	regional func(region string) regional
	tunnels  tunneler
	poll     time.Duration
}

// NewLease is a lease of the profile from the deployment names name, acting
// as cfg's identity and saying what it does through log.
func NewLease(cfg awssdk.Config, names Names, profile Profile, log func(string)) *Lease {
	regionalOf := func(region string) regional { return newRegional(cfg, region) }
	return &Lease{
		names:    names,
		profile:  profile,
		log:      log,
		global:   newGlobal(cfg),
		regional: regionalOf,
		tunnels:  &ssmTunnels{ssm: func(region string) ssmAPI { return regionalOf(region).ssm }},
		poll:     pollInterval,
	}
}

// offer is a zone an instance can be launched in, how, and at what price.
type offer struct {
	region, zone, subnet, group string
	market                      market
	price                       float64
}

func (o offer) String() string { return fmt.Sprintf("%s in %s at $%.3f/h", o.market, o.zone, o.price) }

// placed is the instance a lease launched, where, and its region's clients.
type placed struct {
	id string
	offer
	regional
}

// Run takes the lease for job.
func (l *Lease) Run(ctx context.Context, job Job) (err error) {
	if err := l.tunnels.ready(); err != nil {
		return err
	}
	offers, err := l.offers(ctx)
	if err != nil {
		return err
	}
	instance, err := l.launch(ctx, offers)
	if err != nil {
		return err
	}
	started := time.Now()
	var tunnel tunnel
	defer func() {
		// Termination first, skipping the OS's orderly shutdown, which nothing
		// on the instance needs, on a context of its own: the lease's may be
		// what ended it. Billing ends as the state leaves running; EC2 takes
		// minutes more to remove the instance, which nothing here waits for.
		terminated, terminateErr := instance.ec2.TerminateInstances(context.Background(), &ec2.TerminateInstancesInput{
			InstanceIds: []string{instance.id}, SkipOsShutdown: awssdk.Bool(true),
		})
		ran := time.Since(started)
		if tunnel != nil {
			tunnel.close()
		}
		if terminateErr != nil {
			err = errors.Join(err, fmt.Errorf("terminate %s in %s: %w — terminate it by hand", instance.id, instance.region, terminateErr))
			return
		}
		l.log(fmt.Sprintf("%s %s, billing stopped; it ran %.0f min: ~$%.2f at $%.3f/h",
			instance.id, terminated.TerminatingInstances[0].CurrentState.Name, ran.Minutes(), ran.Hours()*instance.price, instance.price))
	}()

	state, console := "", ""
	for {
		described, err := l.describe(ctx, instance)
		if err != nil {
			return err
		}
		if name := string(described.State.Name); name != state {
			state = name
			l.log(fmt.Sprintf("%s: %s", instance.id, state))
		}
		if state != string(ec2types.InstanceStateNamePending) {
			if console, err = l.tail(ctx, instance, console); err != nil {
				return err
			}
		}
		if state != string(ec2types.InstanceStateNamePending) && state != string(ec2types.InstanceStateNameRunning) {
			reason := "no reason given"
			if described.StateReason != nil {
				reason = awssdk.ToString(described.StateReason.Message)
			}
			return fmt.Errorf("%s is %s: %s — a spot reclaim, or the instance ending itself (its last console lines are above; the whole console: `aws ec2 get-console-output --region %s --latest --instance-id %s`). Run the same command again: it resumes",
				instance.id, state, reason, instance.region, instance.id)
		}
		if tunnel != nil && tunnel.closed() {
			l.log("the tunnel closed: " + tunnel.reason())
			tunnel = nil
		}
		if tunnel == nil && state == string(ec2types.InstanceStateNameRunning) {
			online, err := l.online(ctx, instance)
			if err != nil {
				return err
			}
			if online {
				// An agent just registered may not take a session yet: what
				// did not open is opened on the next pass.
				opened, err := l.tunnels.open(ctx, instance.region, instance.id)
				if err != nil {
					l.log(fmt.Sprintf("the tunnel did not open: %v", err))
				} else {
					tunnel = opened
					l.log(fmt.Sprintf("SSM agent online; tunnel %s → %s:%d", tunnel.url(), instance.id, vllmPort))
				}
			}
		}
		if tunnel != nil && serving(ctx, tunnel.url()) {
			l.log(fmt.Sprintf("%s serving, %.0f min after launch; translating", l.profile.Model, time.Since(started).Minutes()))
			// Session Manager ends a session once it has had no input from the
			// client for a while — 20 minutes unless the account says
			// otherwise — and an answer can take longer than that to come
			// back with nothing sent meanwhile: while the job runs, /health is
			// looked at through the tunnel every poll.
			url, stop := tunnel.url(), make(chan struct{})
			var looking sync.WaitGroup
			looking.Go(func() {
				for {
					select {
					case <-stop:
						return
					case <-time.After(l.poll):
						serving(ctx, url)
					}
				}
			})
			jobErr := job(ctx, url)
			close(stop)
			looking.Wait()
			if jobErr == nil || ctx.Err() != nil {
				return jobErr
			}
			if !tunnel.closed() {
				described, err := l.describe(ctx, instance)
				if err != nil {
					return errors.Join(jobErr, err)
				}
				if described.State.Name == ec2types.InstanceStateNameRunning {
					return jobErr
				}
			}
			// The next pass finds the instance gone, or opens a new tunnel.
			l.log(fmt.Sprintf("the run stopped with the tunnel (%v); running it again", jobErr))
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(l.poll):
		}
	}
}

// describe is the instance as EC2 has it now.
func (l *Lease) describe(ctx context.Context, instance placed) (ec2types.Instance, error) {
	out, err := instance.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{instance.id}})
	if err != nil {
		return ec2types.Instance{}, err
	}
	return out.Reservations[0].Instances[0], nil
}

// online reports whether the instance's SSM agent answers.
func (l *Lease) online(ctx context.Context, instance placed) (bool, error) {
	out, err := instance.ssm.DescribeInstanceInformation(ctx, &ssm.DescribeInstanceInformationInput{
		Filters: []ssmtypes.InstanceInformationStringFilter{{Key: awssdk.String("InstanceIds"), Values: []string{instance.id}}},
	})
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(out.InstanceInformationList, func(node ssmtypes.InstanceInformation) bool {
		return node.PingStatus == ssmtypes.PingStatusOnline
	}), nil
}

// serving reports whether vLLM answers its /health — not yet while it loads,
// nor while the tunnel is not listening.
func serving(ctx context.Context, baseURL string) bool {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode/100 == 2
}

// tail logs the lines the boot script added to the instance's console since
// read, the console as last read, and returns it as read now. EC2 returns the
// latest 64 KB, so what is new is what follows the end of read; a line still
// being written waits for the next look.
func (l *Lease) tail(ctx context.Context, instance placed, read string) (string, error) {
	out, err := instance.ec2.GetConsoleOutput(ctx, &ec2.GetConsoleOutputInput{InstanceId: awssdk.String(instance.id), Latest: awssdk.Bool(true)})
	if err != nil {
		return "", err
	}
	decoded, err := base64.StdEncoding.DecodeString(awssdk.ToString(out.Output))
	if err != nil {
		return "", fmt.Errorf("%s's console: %w", instance.id, err)
	}
	output := string(decoded)
	output = output[:strings.LastIndex(output, "\n")+1]
	anchor := read[max(0, len(read)-512):]
	from := 0
	if at := strings.LastIndex(output, anchor); anchor != "" && at >= 0 {
		from = at + len(anchor)
	}
	for _, line := range strings.Split(output[from:], "\n") {
		if text, ok := strings.CutPrefix(line, consoleTag); ok {
			l.log("   | " + text)
		}
	}
	return output, nil
}

// regionOffers are a region's offers; the markets whose quota there is short
// of one instance; and those whose price could not be read, and why.
type regionOffers struct {
	offers   []offer
	short    []market
	unpriced map[market]error
}

// offers are every zone the profile's instance type can be launched in now,
// cheapest first, from the regions setup prepared, read all at once. A region
// that cannot be read, or a market of one whose price cannot, is said and
// passed over; an instance of the deployment pending or running in any
// refuses the lease.
func (l *Lease) offers(ctx context.Context) ([]offer, error) {
	account, err := callerAccount(ctx, l.sts)
	if err != nil {
		return nil, err
	}
	arn := l.names.PolicyARN(account)
	document, err := policyDocument(ctx, l.iam, arn)
	if absent, err := missing(err); err != nil {
		return nil, err
	} else if absent {
		return nil, fmt.Errorf("no operator policy %s: `translate-gpu setup` prepares the account", arn)
	}
	regions, err := regionsIn(document)
	if err != nil {
		return nil, err
	}
	results, failures, err := eachRegion(ctx, regions, func(region string) (regionOffers, error) { return l.offersIn(ctx, region) })
	if err != nil {
		return nil, err
	}
	var all []offer
	unread := 0
	short := map[market][]string{}
	for i, region := range regions {
		var other *runningError
		switch {
		case errors.As(failures[i], &other):
			return nil, failures[i]
		case failures[i] != nil:
			unread++
			l.log(fmt.Sprintf("%s: passed over, not read: %v", region, failures[i]))
		default:
			all = append(all, results[i].offers...)
			for _, m := range results[i].short {
				short[m] = append(short[m], region)
			}
			for _, quota := range quotas {
				if err, ok := results[i].unpriced[quota.market]; ok {
					l.log(fmt.Sprintf("%s: %s passed over, its price not read: %v", region, quota.market, err))
				}
			}
		}
	}
	for _, quota := range quotas {
		if in := short[quota.market]; len(in) > 0 {
			l.log(fmt.Sprintf("%s quota short of one %s in %s", quota.market, l.profile.InstanceType, strings.Join(in, ", ")))
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("nowhere to launch a %s now: of the %d region(s) setup prepared, %d could not be read, and none of the others offers it with its price read and the quota for one — each is said above", l.profile.InstanceType, len(regions), unread)
	}
	slices.SortFunc(all, func(a, b offer) int {
		return cmp.Or(cmp.Compare(a.price, b.price), cmp.Compare(a.region, b.region), cmp.Compare(a.zone, b.zone), cmp.Compare(a.market, b.market))
	})
	l.log(fmt.Sprintf("%d place(s) to launch a %s in %d region(s); cheapest %s", len(all), l.profile.InstanceType, len(regions), all[0]))
	return all, nil
}

// offersIn are a region's offers: each zone offering the instance type that
// has a default subnet, spot where the region's spot quota holds one instance
// and on demand where its on-demand quota does, each market as far as its
// price can be read.
func (l *Lease) offersIn(ctx context.Context, region string) (regionOffers, error) {
	clients := l.regional(region)
	own, err := clients.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{Filters: []ec2types.Filter{{Name: awssdk.String("tag:Project"), Values: []string{l.names.Tag()}}}})
	if err != nil {
		return regionOffers{}, err
	}
	if err := running(own.Reservations, region); err != nil {
		return regionOffers{}, err
	}
	groups, err := securityGroups(ctx, clients.ec2, l.names)
	if err != nil {
		return regionOffers{}, err
	}
	if len(groups) == 0 {
		return regionOffers{}, fmt.Errorf("no security group %s: `translate-gpu setup` prepares a region", l.names.SecurityGroup())
	}
	o, err := offeringIn(ctx, clients, []string{l.profile.InstanceType})
	if err != nil || len(o.zones) == 0 {
		return regionOffers{}, err
	}
	// The default VPC's subnets give the instance the public address it
	// reaches Systems Manager, Docker Hub and Hugging Face through.
	described, err := clients.ec2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{Filters: []ec2types.Filter{{Name: awssdk.String("default-for-az"), Values: []string{"true"}}}})
	if err != nil {
		return regionOffers{}, err
	}
	subnets := map[string]string{}
	for _, subnet := range described.Subnets {
		if zone := awssdk.ToString(subnet.AvailabilityZone); o.zones[zone] {
			subnets[zone] = awssdk.ToString(subnet.SubnetId)
		}
	}
	found := regionOffers{unpriced: map[market]error{}}
	for _, quota := range quotas {
		if !o.holds(quota.market) {
			found.short = append(found.short, quota.market)
			continue
		}
		prices, err := l.prices(ctx, clients.ec2, region, quota.market, slices.Collect(maps.Keys(subnets)))
		if err != nil {
			found.unpriced[quota.market] = err
			continue
		}
		for zone, price := range prices {
			if subnet, ok := subnets[zone]; ok {
				found.offers = append(found.offers, offer{region: region, zone: zone, subnet: subnet, group: awssdk.ToString(groups[0].GroupId), market: quota.market, price: price})
			}
		}
	}
	return found, nil
}

// prices are the instance type's hourly prices in a region's zones on a
// market: spot's now, by zone, and on demand the Pricing API's, the same in
// each zone.
func (l *Lease) prices(ctx context.Context, client ec2API, region string, m market, zones []string) (map[string]float64, error) {
	if m == spot {
		return spotPrices(ctx, client, l.profile.InstanceType)
	}
	price, err := l.onDemandPrice(ctx, region)
	if err != nil {
		return nil, err
	}
	prices := map[string]float64{}
	for _, zone := range zones {
		prices[zone] = price
	}
	return prices, nil
}

// runningError is an instance of the deployment pending or running: another
// lease's, or one whose launcher died.
type runningError struct{ instance, state, region string }

func (e *runningError) Error() string {
	return fmt.Sprintf("%s is %s in %s: another lease's, or one whose launcher died — that one ends itself at its idle limit", e.instance, e.state, e.region)
}

// running refuses a lease while any of the instances is pending or running.
func running(reservations []ec2types.Reservation, region string) error {
	for _, reservation := range reservations {
		for _, instance := range reservation.Instances {
			if instance.State.Name == ec2types.InstanceStateNamePending || instance.State.Name == ec2types.InstanceStateNameRunning {
				return &runningError{instance: awssdk.ToString(instance.InstanceId), state: string(instance.State.Name), region: region}
			}
		}
	}
	return nil
}

// launch starts the instance at the first of the offers whose zone has the
// capacity.
func (l *Lease) launch(ctx context.Context, offers []offer) (placed, error) {
	clients := map[string]regional{}
	images := map[string]string{}
	awaited := map[string]bool{}
	// A region's market whose quota other instances have spent.
	type regionMarket struct {
		region string
		market market
	}
	spent := map[regionMarket]bool{}
	tags := func(kind ec2types.ResourceType) ec2types.TagSpecification {
		return ec2types.TagSpecification{ResourceType: kind, Tags: []ec2types.Tag{{Key: awssdk.String("Project"), Value: awssdk.String(l.names.Tag())}}}
	}
	for _, o := range offers {
		where := regionMarket{o.region, o.market}
		if spent[where] {
			continue
		}
		c, ok := clients[o.region]
		if !ok {
			c = l.regional(o.region)
			clients[o.region] = c
			parameter, err := c.ssm.GetParameter(ctx, &ssm.GetParameterInput{Name: awssdk.String(dlami)})
			if err != nil {
				return placed{}, fmt.Errorf("%s: %w", o.region, err)
			}
			images[o.region] = awssdk.ToString(parameter.Parameter.Value)
		}
		var options *ec2types.InstanceMarketOptionsRequest
		if o.market == spot {
			if !awaited[o.region] {
				if err := l.awaitSpotQuota(ctx, c.ec2, o.region); err != nil {
					return placed{}, err
				}
				awaited[o.region] = true
			}
			options = &ec2types.InstanceMarketOptionsRequest{
				MarketType: ec2types.MarketTypeSpot,
				SpotOptions: &ec2types.SpotMarketOptions{
					SpotInstanceType:             ec2types.SpotInstanceTypeOneTime,
					InstanceInterruptionBehavior: ec2types.InstanceInterruptionBehaviorTerminate,
				},
			}
		}
		out, err := c.ec2.RunInstances(ctx, &ec2.RunInstancesInput{
			ImageId:                           awssdk.String(images[o.region]),
			InstanceType:                      ec2types.InstanceType(l.profile.InstanceType),
			MinCount:                          awssdk.Int32(1),
			MaxCount:                          awssdk.Int32(1),
			SubnetId:                          awssdk.String(o.subnet),
			SecurityGroupIds:                  []string{o.group},
			IamInstanceProfile:                &ec2types.IamInstanceProfileSpecification{Name: awssdk.String(l.names.InstanceProfile())},
			MetadataOptions:                   &ec2types.InstanceMetadataOptionsRequest{HttpTokens: ec2types.HttpTokensStateRequired, HttpEndpoint: ec2types.InstanceMetadataEndpointStateEnabled},
			InstanceInitiatedShutdownBehavior: ec2types.ShutdownBehaviorTerminate,
			TagSpecifications:                 []ec2types.TagSpecification{tags(ec2types.ResourceTypeInstance), tags(ec2types.ResourceTypeVolume)},
			UserData:                          awssdk.String(base64.StdEncoding.EncodeToString([]byte(l.profile.userData()))),
			InstanceMarketOptions:             options,
		})
		switch refusals[errorCode(err)] {
		case zoneRefused:
			l.log(fmt.Sprintf("%s %s: %v", o.market, o.zone, err))
			continue
		case marketRefused:
			spent[where] = true
			l.log(fmt.Sprintf("%s in %s: %v — its quota spent by other instances, its other zones are passed over", o.market, o.region, err))
			continue
		}
		if err != nil {
			return placed{}, fmt.Errorf("%s: %w", o.zone, err)
		}
		instance := placed{id: awssdk.ToString(out.Instances[0].InstanceId), offer: o, regional: c}
		l.log(fmt.Sprintf("%s: %s %s, %s", instance.id, l.profile.InstanceType, o, images[o.region]))
		return instance, nil
	}
	return placed{}, fmt.Errorf("nowhere would launch a %s now, spot or on demand — each refusal is said above; run again later", l.profile.InstanceType)
}

// awaitSpotQuota waits until no spot request of an earlier lease holds the
// region's quota. EC2 counts a request against it until it closes it, minutes
// after its instance was terminated: a lease taken right after the last one
// ended waits for that rather than being refused. An instance of an earlier
// lease still running is refused outright.
func (l *Lease) awaitSpotQuota(ctx context.Context, client ec2API, region string) error {
	waiting := false
	for {
		described, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{Filters: []ec2types.Filter{
			{Name: awssdk.String("tag:Project"), Values: []string{l.names.Tag()}},
			{Name: awssdk.String("instance-lifecycle"), Values: []string{"spot"}},
		}})
		if err != nil {
			return err
		}
		if err := running(described.Reservations, region); err != nil {
			return err
		}
		requests := map[string]string{}
		for _, reservation := range described.Reservations {
			for _, previous := range reservation.Instances {
				requests[awssdk.ToString(previous.SpotInstanceRequestId)] = awssdk.ToString(previous.InstanceId)
			}
		}
		var held []string
		if len(requests) > 0 {
			open, err := client.DescribeSpotInstanceRequests(ctx, &ec2.DescribeSpotInstanceRequestsInput{Filters: []ec2types.Filter{
				{Name: awssdk.String("spot-instance-request-id"), Values: slices.Sorted(maps.Keys(requests))},
				{Name: awssdk.String("state"), Values: []string{"open", "active"}},
			}})
			if err != nil {
				return err
			}
			for _, request := range open.SpotInstanceRequests {
				held = append(held, requests[awssdk.ToString(request.SpotInstanceRequestId)])
			}
		}
		if len(held) == 0 {
			return nil
		}
		if !waiting {
			l.log(fmt.Sprintf("waiting for EC2 to release the spot quota %s held in %s; it takes minutes after termination", strings.Join(held, ", "), region))
			waiting = true
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(l.poll):
		}
	}
}

// spotPrices are an instance type's spot price now in a region, by zone.
func spotPrices(ctx context.Context, client ec2API, instanceType string) (map[string]float64, error) {
	now := time.Now().UTC()
	history, err := client.DescribeSpotPriceHistory(ctx, &ec2.DescribeSpotPriceHistoryInput{
		InstanceTypes:       toInstanceTypes([]string{instanceType}),
		ProductDescriptions: []string{"Linux/UNIX"},
		StartTime:           awssdk.Time(now),
		EndTime:             awssdk.Time(now),
	})
	if err != nil {
		return nil, err
	}
	prices := map[string]float64{}
	for _, entry := range history.SpotPriceHistory {
		price, err := strconv.ParseFloat(awssdk.ToString(entry.SpotPrice), 64)
		if err != nil {
			return nil, fmt.Errorf("spot price %q: %w", awssdk.ToString(entry.SpotPrice), err)
		}
		prices[awssdk.ToString(entry.AvailabilityZone)] = price
	}
	return prices, nil
}

// onDemandPrice is the instance type's on-demand price in a region, as the
// Pricing API states it.
func (l *Lease) onDemandPrice(ctx context.Context, region string) (float64, error) {
	var filters []pricingtypes.Filter
	for _, f := range [][2]string{
		{"instanceType", l.profile.InstanceType}, {"regionCode", region}, {"operatingSystem", "Linux"},
		{"tenancy", "Shared"}, {"preInstalledSw", "NA"}, {"capacitystatus", "Used"},
	} {
		filters = append(filters, pricingtypes.Filter{Type: pricingtypes.FilterTypeTermMatch, Field: awssdk.String(f[0]), Value: awssdk.String(f[1])})
	}
	products, err := l.pricing.GetProducts(ctx, &pricing.GetProductsInput{ServiceCode: awssdk.String("AmazonEC2"), Filters: filters})
	if err != nil {
		return 0, err
	}
	if len(products.PriceList) != 1 {
		return 0, fmt.Errorf("the Pricing API lists %d on-demand products for %s in %s, not one", len(products.PriceList), l.profile.InstanceType, region)
	}
	var product struct {
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					PricePerUnit struct {
						USD string `json:"USD"`
					} `json:"pricePerUnit"`
				} `json:"priceDimensions"`
			} `json:"OnDemand"`
		} `json:"terms"`
	}
	if err := json.Unmarshal([]byte(products.PriceList[0]), &product); err != nil {
		return 0, fmt.Errorf("the Pricing API's product: %w", err)
	}
	term, ok := only(product.Terms.OnDemand)
	if ok {
		if dimension, ok := only(term.PriceDimensions); ok {
			return strconv.ParseFloat(dimension.PricePerUnit.USD, 64)
		}
	}
	return 0, fmt.Errorf("the Pricing API's product for %s in %s has no single on-demand price", l.profile.InstanceType, region)
}

// only is a map's one value, where it has exactly one.
func only[V any](m map[string]V) (V, bool) {
	for _, v := range m {
		if len(m) == 1 {
			return v, true
		}
	}
	var zero V
	return zero, false
}
