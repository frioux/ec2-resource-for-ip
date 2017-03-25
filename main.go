package main

import (
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

var (
	verbose = flag.Bool("verbose", false, "Never stop talking")
)

func main() {
	flag.Parse()

	sess, err := session.NewSession()
	if err != nil {
		panic(err)
	}

	regions := []string{"us-west-1", "us-east-1"}

	ips := flag.Args()

	foundIps := make(map[string]bool)
	for _, ip := range ips {
		foundIps[ip] = false
	}

	for _, region := range regions {
		found, err := ec2_instance_public(region, sess, ips)

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

		found, err = ec2_instance_private(region, sess, ips)

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

		found, err = eip(region, sess, ips)
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

	}
	for ip := range foundIps {
		fmt.Printf("%s:\n", ip)
	}
}

func elb(ip string) {}

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

func unknown(ip string) {}

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
					"  id: %s\n", region, *instance.InstanceId)
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
					"  id: %s\n", region, *instance.InstanceId)
		}
	}
	return ret, nil
}
