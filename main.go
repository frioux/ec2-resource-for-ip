package main

import (
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elb"
	"net"
	"strings"
	"sync"
)

var (
	verbose = flag.Bool("verbose", false, "Never stop talking")
)

type ipLookup struct {
	name string
	result string
	err error
}

func allRegions(sess *session.Session) ([]string, error) {
	svc := ec2.New(sess, &aws.Config{Region: aws.String("us-west-1")})

	ret := []string{}

	resp, err := svc.DescribeRegions(nil)
	if err != nil {
		return []string{"us-west-1", "us-east-1", "us-west-2"}, err
	}

	for _, region := range resp.Regions {
		ret = append(ret, *region.RegionName)
	}
	return ret, nil
}

func main() {
	flag.Parse()

	sess, err := session.NewSession()
	if err != nil {
		panic(err)
	}

	regions, err := allRegions(sess)
	if *verbose {
		fmt.Println("Checking", regions)
	}
	if err != nil {
		if *verbose {
			fmt.Println(err)
		}
	}

	ips := flag.Args()

	out := make(chan ipLookup)
	errC := make(chan error)

	done := make(chan bool)

	var wg sync.WaitGroup
	var workerWg sync.WaitGroup

	wg.Add(len(ips))
	workerWg.Add(1)
	foundIps := make(map[string]bool)
	for _, ip := range ips {
		foundIps[ip] = false
	}

	go func() {
		workerWg.Wait()
		close(done)
	}()

	go func() {
		wg.Wait()
		close(done)
	}()

	go func() {
		for err := range errC {
			if err != nil {
				if *verbose {
					fmt.Println(err)
				}
			}
		}
	}()

	go func() {
		for i := range out {
			if i.err != nil {
				if *verbose {
					fmt.Println(i.err)
				}
			} else {
				delete(foundIps, i.name)
				fmt.Print(i.name + ":\n" + i.result)
			}
			// XXX: need to verify that the thing found actually deleted a
			// record, or something
			wg.Done()
		}
	}()

	for _, region := range regions {
		region := region

		go func() {
			workerWg.Add(1)
			errC <- ec2_instance_public(region, sess, ips, out)
			workerWg.Done()
		}()
		go func() {
			workerWg.Add(1)
			errC <- ec2_instance_private(region, sess, ips, out)
			workerWg.Done()
		}()
		go func() {
			workerWg.Add(1)
			errC <- eip(region, sess, ips, out)
			workerWg.Done()
		}()
		go func() {
			workerWg.Add(1)
			errC <- find_elb(region, sess, ips, errC, out)
			workerWg.Done()
		}()
	}
	workerWg.Done()

	for range done {

	}

	keys := make([]string, len(foundIps))
	i := 0
	for k := range foundIps {
		keys[i] = k
		i++
	}

	found, err := unknown(keys)
	if err != nil {
		if *verbose {
			fmt.Println(err)
		}
	} else {
		for ip, str := range found {
			delete(foundIps, ip)
			fmt.Print(ip + ":\n" + str)
		}
	}

	for ip := range foundIps {
		fmt.Printf("%s:\n", ip)
	}
}

func find_elb(region string, sess *session.Session, ips []string, errC chan error, out chan ipLookup) error {
	svc := elb.New(sess, &aws.Config{Region: aws.String(region)})

	// map of ip to elb-id
	lookup := make(map[string]string)

	resp, err := svc.DescribeLoadBalancers(nil)

	if err != nil {
		return err
	}

	for _, lb := range resp.LoadBalancerDescriptions {
		ips, err := net.LookupIP(*lb.DNSName)

		// This happens all the time; do not early exit
		if err != nil {
			errC <- err
			continue
		}

		name := *lb.LoadBalancerName
		for _, ip := range ips {
			lookup[ip.String()] = name
		}
	}

	for _, ip := range ips {
		if name, ok := lookup[ip]; ok {
			out <- ipLookup{
				name: ip,
				result: fmt.Sprintf(
					"  type: elb\n"+
						"  region: %s\n"+
						"  name: %s\n", region, name),
			}
		}
	}

	return nil
}

func eip(region string, sess *session.Session, ips []string, out chan ipLookup) error {
	svc := ec2.New(sess, &aws.Config{Region: aws.String(region)})

	awsIps := []*string{}
	for _, ip := range ips {
		awsIps = append(awsIps, aws.String(ip))
	}

	params := &ec2.DescribeAddressesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("public-ip"),
				Values: awsIps,
			},
		},
	}

	resp, err := svc.DescribeAddresses(params)
	if err != nil {
		return err
	}

	for _, address := range resp.Addresses {
		id := address.AllocationId
		if id == nil {
			continue
		}
		out <- ipLookup{
			name: *address.PublicIp,
			result: fmt.Sprintf(
				"  type: eip\n"+
					"  region: %s\n"+
					"  id: %s\n", region, *id),
		}
	}
	return nil
}

func toptr(ip string) string {
	parts := strings.Split(ip, ".")

	ret := ""
	for i := len(parts) - 1; i > 0; i-- {
		ret += parts[i] + "."
	}
	ret += parts[0] + ".in-addr.arpa"

	return ret
}

func unknown(ips []string) (map[string]string, error) {
	ret := make(map[string]string)
	for _, ip := range ips {
		ptrs, err := net.LookupAddr(ip)
		if err != nil {
			if *verbose {
				fmt.Println(err)
			}
			ptrs = []string{""}
		}
		ret[ip] = fmt.Sprintf(
			"  type: unknown\n"+
				"  ptr: %s\n", ptrs[0])
	}
	return ret, nil
}

func getEC2Name(i *ec2.Instance) string {
	for _, tag := range i.Tags {
		if *tag.Key == "Name" {
			return *tag.Value
		}
	}
	return ""
}

func ec2_instance_public(region string, sess *session.Session, ips []string, out chan ipLookup) error {
	svc := ec2.New(sess, &aws.Config{Region: aws.String(region)})

	awsIps := []*string{}
	for _, ip := range ips {
		awsIps = append(awsIps, aws.String(ip))
	}

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("ip-address"),
				Values: awsIps,
			},
		},
	}
	resp, err := svc.DescribeInstances(params)

	if err != nil {
		return err
	}

	for _, res := range resp.Reservations {
		for _, instance := range res.Instances {
			out <- ipLookup{
				name: *instance.PublicIpAddress,
				result: fmt.Sprintf(
				"  type: ec2_instance\n"+
					"  region: %s\n"+
					"  id: %s\n"+
					"  name: %s\n",
				region, *instance.InstanceId, getEC2Name(instance)),
			}
		}
	}
	return nil
}

func ec2_instance_private(region string, sess *session.Session, ips []string, out chan ipLookup) error {
	svc := ec2.New(sess, &aws.Config{Region: aws.String(region)})

	awsIps := []*string{}
	for _, ip := range ips {
		awsIps = append(awsIps, aws.String(ip))
	}

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("private-ip-address"),
				Values: awsIps,
			},
		},
	}
	resp, err := svc.DescribeInstances(params)

	if err != nil {
		return err
	}

	for _, res := range resp.Reservations {
		for _, instance := range res.Instances {
			out <- ipLookup{
				name: *instance.PrivateIpAddress,
				result: fmt.Sprintf(
				"  type: ec2_instance\n"+
					"  region: %s\n"+
					"  id: %s\n"+
					"  name: %s\n",
				region, *instance.InstanceId, getEC2Name(instance)),
			}
		}
	}
	return nil
}
