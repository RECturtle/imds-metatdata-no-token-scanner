package main

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type MockRetrieveInstancePager struct {
	PageNum int
	Pages   []*ec2.DescribeInstancesOutput
}

func (m *MockRetrieveInstancePager) HasMorePages() bool {
	return m.PageNum < len(m.Pages)
}

func (m *MockRetrieveInstancePager) NextPage(ctx context.Context, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if m.PageNum > len(m.Pages) {
		return nil, fmt.Errorf("no more pages")
	}
	output := m.Pages[m.PageNum]
	m.PageNum++
	return output, nil
}

type MockEc2DescribeRegions struct{}

func (m *MockEc2DescribeRegions) DescribeRegions(ctx context.Context, params *ec2.DescribeRegionsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error) {
	uw2, ue1 := "us-west-2", "us-east-1"
	result := &ec2.DescribeRegionsOutput{Regions: []types.Region{{RegionName: &uw2}, {RegionName: &ue1}}}
	return result, nil
}

func TestRetrieveRegions(t *testing.T) {
	ctx := context.TODO()
	uw2, ue1 := "us-west-2", "us-east-1"
	want := []types.Region{{RegionName: &uw2}, {RegionName: &ue1}}
	got := retrieveRegions(ctx, &MockEc2DescribeRegions{})

	reflect.DeepEqual(got, want)
}

func TestAddInstance(t *testing.T) {
	ri := RegionInstances{"us-west-2", []*Ec2Instance{}}
	ri.addInstance(&Ec2Instance{"123", 2.0})
	want := RegionInstances{"us-west-2", []*Ec2Instance{{"123", 2.0}}}

	reflect.DeepEqual(ri, want)
}

func TestRetrieveInstances(t *testing.T) {
	ctx := context.TODO()
	ione, itwo := "123", "234"
	pager := &MockRetrieveInstancePager{
		Pages: []*ec2.DescribeInstancesOutput{
			{Reservations: []types.Reservation{{Instances: []types.Instance{{InstanceId: &ione}}}}},
			{Reservations: []types.Reservation{{Instances: []types.Instance{{InstanceId: &itwo}}}}},
		},
	}

	ri := RegionInstances{"us-west-2", []*Ec2Instance{}}
	ri.retrieveInstances(ctx, pager)
	want := RegionInstances{"us-west-2", []*Ec2Instance{{"123", 2.0}, {"234", 2.0}}}
	reflect.DeepEqual(ri, want)
}
