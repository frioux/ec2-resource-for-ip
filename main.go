package main

import (
	"errors"
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
	for _, ip := range flag.Args() {
		fmt.Printf("%s:\n", ip)
		for _, region := range regions {
			ec2_instance(region, sess, ip)

			str, err := eip(region, sess, ip)
			if err != nil {
				if *verbose {
					fmt.Println(err)
				}
			} else {
				fmt.Println(str)
			}
		}
	}
}

func elb(ip string) {}

func eip(region string, sess *session.Session, ip string) (string, error) {
	str, err := ec2_instance_public(region, sess, ip)

	if err != nil {
		if *verbose {
			fmt.Println(err)
		}
	} else {
		fmt.Println(str)
	}

	svc := ec2.New(sess, &aws.Config{Region: aws.String(region)})

	params := &ec2.DescribeAddressesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("public-ip"),
				Values: []*string{
					aws.String(ip),
				},
			},
		},
	}
	resp, err := svc.DescribeAddresses(params)
	if err != nil {
		return "", err
	}

	for _, address := range resp.Addresses {
		id := address.AllocationId
		return fmt.Sprintf(
			"  type: eip\n" +
			"  region: %s\n" +
			"  id: %s\n", region, *id), nil
	}
	return "", errors.New("EIP not found in " + region)
}

func unknown(ip string) {}

func ec2_instance(region string, sess *session.Session, ip string) {
	str, err := ec2_instance_public(region, sess, ip)

	if err != nil {
		if *verbose {
			fmt.Println(err)
		}
	} else {
		fmt.Println(str)
	}

	str, err = ec2_instance_private(region, sess, ip)

	if err != nil {
		if *verbose {
			fmt.Println(err)
		}
	} else {
		fmt.Println(str)
	}
}

func ec2_instance_private(region string, sess *session.Session, ip string) (string, error) {
	svc := ec2.New(sess, &aws.Config{Region: aws.String(region)})

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("private-ip-address"),
				Values: []*string{
					aws.String(ip),
				},
			},
		},
	}
	resp, err := svc.DescribeInstances(params)

	if err != nil {
		return "", err
	}

	for _, res := range resp.Reservations {
		for _, instance := range res.Instances {
			i := instance.InstanceId
			return fmt.Sprintf(
				"  type: ec2_instance\n" +
				"  region: %s\n" +
				"  id: %s\n", region, *i), nil
		}
	}
	return "", errors.New("None Found")
}

func ec2_instance_public(region string, sess *session.Session, ip string) (string, error) {
	svc := ec2.New(sess, &aws.Config{Region: aws.String(region)})

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("ip-address"),
				Values: []*string{
					aws.String(ip),
				},
			},
		},
	}
	resp, err := svc.DescribeInstances(params)

	if err != nil {
		return "", err
	}

	for _, res := range resp.Reservations {
		for _, instance := range res.Instances {
			i := instance.InstanceId
			return fmt.Sprintf(
				"  type: ec2_instance\n" +
				"  region: %s\n" +
				"  id: %s\n", region, *i), nil
		}
	}
	return "", errors.New("None Found")
}
