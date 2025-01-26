package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwTypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
)

var (
	today           = time.Now()
	startDate       = time.Now().AddDate(0, 0, -450)
	period    int32 = 38880000
)

type RegionInstances struct {
	region    string
	instances []*Ec2Instance
}

type Ec2Instance struct {
	instanceId           string
	metadataNoTokenCalls float64
}

type ec2DescribeRegions interface {
	DescribeRegions(ctx context.Context, params *ec2.DescribeRegionsInput, optsFns ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error)
}

type ec2DescribeInstancesPaginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context, optsFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
}

func main() {
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)

	if err != nil {
		log.Fatalf("unable to load AWS SDK config: %v", err)
	}

	cfg.Region = "us-west-2"

	writer, file, err := openCSV("instances.csv")
	defer file.Close()
	writeToCSV(writer, []string{"region", "instance-id", "imdsv1 calls"})

	ec2Client := ec2.NewFromConfig(cfg)
	regions := retrieveRegions(ctx, ec2Client)

	for _, region := range regions {
		if region.RegionName == nil {
			continue
		}

		regionInstances := RegionInstances{region: *region.RegionName}
		cfg.Region = regionInstances.region

		fmt.Printf("=========== %s ===========\n", regionInstances.region)
		regionalEc2Client := ec2.NewFromConfig(cfg)
		ec2Paginator := ec2.NewDescribeInstancesPaginator(regionalEc2Client, &ec2.DescribeInstancesInput{})
		err := regionInstances.retrieveInstances(ctx, ec2Paginator)

		if err != nil {
			continue
		}

		if len(regionInstances.instances) == 0 {
			slog.Info("no ec2 instances found", "region", regionInstances.region)
			continue
		}

		regionInstances.retrieveCloudwatchMetrics(ctx, cfg)
		fmt.Printf("====================== %s instances with metadatanotoken metric greater than 0 ======================\n", regionInstances.region)
		for _, v := range regionInstances.instances {
			if v.metadataNoTokenCalls > 0 {
				fmt.Printf("Instance Id: %v | MetadataNoToken Calls: %v\n", v.instanceId, v.metadataNoTokenCalls)
			}
		}

		for _, e := range regionInstances.instances {
			var instanceRow []string
			instanceRow = append(instanceRow, *region.RegionName)
			instanceRow = append(instanceRow, e.instanceId)
			instanceRow = append(instanceRow, strconv.FormatFloat(e.metadataNoTokenCalls, 'f', 2, 64))
			writeToCSV(writer, instanceRow)
		}
	}
}

func openCSV(filename string) (*csv.Writer, *os.File, error) {
	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("error opening file: %v, error: %v", filename, err)
	}
	writer := csv.NewWriter(f)
	return writer, f, nil
}

func writeToCSV(w *csv.Writer, instanceRow []string) {
	if err := w.Write(instanceRow); err != nil {
		log.Fatalln("error writing record to csv:", err)
	}

	if err := w.Error(); err != nil {
		log.Fatal(err)
	}
}

func retrieveRegions(ctx context.Context, ec2Client ec2DescribeRegions) []ec2Types.Region {
	regions, err := ec2Client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})

	if err != nil {
		log.Fatalf("unable to retrieve regions: %v", err)
	}
	return regions.Regions
}

func (r *RegionInstances) addInstance(instance *Ec2Instance) {
	r.instances = append(r.instances, instance)
}

func (r *RegionInstances) retrieveInstances(ctx context.Context, paginator ec2DescribeInstancesPaginator) error {
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				if ae.ErrorCode() == "UnathorizedOperation" {
					slog.Warn("you are not authorized to perform this action", "error", ae.ErrorMessage())
					return err
				}
			}
			log.Fatalf("failed to retrieve instances: %v", err)
			return nil
		}

		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				r.addInstance(&Ec2Instance{instanceId: *instance.InstanceId, metadataNoTokenCalls: 0.0})
			}
		}
	}
	return nil
}

func (r *RegionInstances) retrieveCloudwatchMetrics(ctx context.Context, cfg aws.Config) {
	cloudwatchCfg := cfg
	cloudwatchCfg.Region = r.region
	cloudwatchClient := cloudwatch.NewFromConfig(cloudwatchCfg)

	for _, instance := range r.instances {
		slog.Info("retrieving cloudwatch metrics for instance", "instance id", instance.instanceId)
		input := &cloudwatch.GetMetricStatisticsInput{
			Namespace:  aws.String("AWS/EC2"),
			MetricName: aws.String("MetadataNoToken"),
			Dimensions: []cwTypes.Dimension{
				{
					Name:  aws.String("InstanceId"),
					Value: aws.String(instance.instanceId),
				},
			},
			StartTime: &startDate,
			EndTime:   &today,
			Period:    &period,
			Statistics: []cwTypes.Statistic{
				cwTypes.StatisticSum,
			},
		}

		res, err := cloudwatchClient.GetMetricStatistics(ctx, input)

		if err != nil {
			log.Fatalf("error retrieving metrics for instance %s, %v", instance.instanceId, err)
		}

		for _, d := range res.Datapoints {
			instance.metadataNoTokenCalls += *d.Sum
		}
	}
}
