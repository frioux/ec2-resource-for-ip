package main

import (
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elb"
	"golang.org/x/net/context"
	"golang.org/x/sync/errgroup"
	"net"
	"strings"
)

var (
	verbose = flag.Bool("verbose", false, "Never stop talking")
)

func showResults(found map[string]string, err error, foundIps *map[string]bool) {
	if err != nil {
		if *verbose {
			fmt.Println(err)
		}
	} else {
		for ip, str := range found {
			delete(*foundIps, ip)
			fmt.Print(ip + ":\n" + str)
		}
	}
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

	foundIps := make(map[string]bool)
	for _, ip := range ips {
		foundIps[ip] = false
	}

	find_ips := func(ctx context.Context, ips []string) (map[string]string, error) {
		g, ctx := errgroup.WithContext(ctx)

		results := make(map[string]string)
		for _, region := range regions {
			region := region

			g.Go(func() error {
				found, err := ec2_instance_public(region, sess, ips)
				for k, v := range found {
					results[k] = v
				}
				return err
			})
			g.Go(func() error {
				found, err := ec2_instance_private(region, sess, ips)
				for k, v := range found {
					results[k] = v
				}
				return err
			})
			g.Go(func() error {
				found, err := eip(region, sess, ips)
				for k, v := range found {
					results[k] = v
				}
				return err
			})
			g.Go(func() error {
				found, err := find_elb(region, sess, ips)
				for k, v := range found {
					results[k] = v
				}
				return err
			})
		}

		err := g.Wait()
		return results, err
	}

	results, err := find_ips(context.Background(), ips)
	showResults(results, err, &foundIps)

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

func find_elb(region string, sess *session.Session, ips []string) (map[string]string, error) {
	svc := elb.New(sess, &aws.Config{Region: aws.String(region)})

	// map of ip to elb-id
	lookup := make(map[string]string)

	resp, err := svc.DescribeLoadBalancers(nil)

	if err != nil {
		return nil, err
	}

	for _, lb := range resp.LoadBalancerDescriptions {
		ips, err := net.LookupIP(*lb.DNSName)

		// This happens all the time; do not early exit
		if err != nil {
			if *verbose {
				fmt.Println(err)
			}
			continue
		}

		name := *lb.LoadBalancerName
		for _, ip := range ips {
			lookup[ip.String()] = name
		}
	}

	ret := make(map[string]string)
	for _, ip := range ips {
		if name, ok := lookup[ip]; ok {
			ret[ip] = fmt.Sprintf(
				"  type: elb\n"+
					"  region: %s\n"+
					"  name: %s\n", region, name)
		}
	}

	return ret, nil
}

func eip(region string, sess *session.Session, ips []string) (map[string]string, error) {
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
		return nil, err
	}

	ret := make(map[string]string)
	for _, address := range resp.Addresses {
		id := address.AllocationId
		ret[*address.PublicIp] = fmt.Sprintf(
			"  type: eip\n"+
				"  region: %s\n"+
				"  id: %s\n", region, *id)
	}
	return ret, nil
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

func ec2_instance_public(region string, sess *session.Session, ips []string) (map[string]string, error) {
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
		return nil, err
	}

	ret := make(map[string]string)

	for _, res := range resp.Reservations {
		for _, instance := range res.Instances {
			ret[*instance.PublicIpAddress] = fmt.Sprintf(
				"  type: ec2_instance\n"+
					"  region: %s\n"+
					"  id: %s\n"+
					"  name: %s\n",
				region, *instance.InstanceId, getEC2Name(instance))
		}
	}
	return ret, nil
}

func ec2_instance_private(region string, sess *session.Session, ips []string) (map[string]string, error) {
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
		return nil, err
	}

	ret := make(map[string]string)

	for _, res := range resp.Reservations {
		for _, instance := range res.Instances {
			ret[*instance.PrivateIpAddress] = fmt.Sprintf(
				"  type: ec2_instance\n"+
					"  region: %s\n"+
					"  id: %s\n"+
					"  name: %s\n",
				region, *instance.InstanceId, getEC2Name(instance))
		}
	}
	return ret, nil
}
