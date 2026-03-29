package main

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func NewAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx, config.WithRegion(region))
}

func NewAWSClients(cfg aws.Config) (*dynamodb.Client, *s3.Client, *sqs.Client) {
	dynamo := dynamodb.NewFromConfig(cfg)
	s3client := s3.NewFromConfig(cfg)
	sqsclient := sqs.NewFromConfig(cfg)
	return dynamo, s3client, sqsclient
}
