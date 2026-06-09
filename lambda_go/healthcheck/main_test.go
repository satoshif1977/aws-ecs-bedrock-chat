package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// ── モック ────────────────────────────────────────────────────────────────────

type mockECSClient struct {
	output *ecs.DescribeServicesOutput
	err    error
}

func (m *mockECSClient) DescribeServices(
	_ context.Context,
	_ *ecs.DescribeServicesInput,
	_ ...func(*ecs.Options),
) (*ecs.DescribeServicesOutput, error) {
	return m.output, m.err
}

type mockELBV2Client struct {
	output *elasticloadbalancingv2.DescribeTargetHealthOutput
	err    error
}

func (m *mockELBV2Client) DescribeTargetHealth(
	_ context.Context,
	_ *elasticloadbalancingv2.DescribeTargetHealthInput,
	_ ...func(*elasticloadbalancingv2.Options),
) (*elasticloadbalancingv2.DescribeTargetHealthOutput, error) {
	return m.output, m.err
}

type mockDynamoDBClient struct {
	output *dynamodb.DescribeTableOutput
	err    error
}

func (m *mockDynamoDBClient) DescribeTable(
	_ context.Context,
	_ *dynamodb.DescribeTableInput,
	_ ...func(*dynamodb.Options),
) (*dynamodb.DescribeTableOutput, error) {
	return m.output, m.err
}

// ── CheckECS ──────────────────────────────────────────────────────────────────

func TestCheckECS_Healthy(t *testing.T) {
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						ClusterArn:   aws.String("arn:aws:ecs:ap-northeast-1:123456789012:cluster/bedrock-chat-dev"),
						ServiceName:  aws.String("bedrock-chat-dev-service"),
						Status:       aws.String("ACTIVE"),
						RunningCount: 2,
						DesiredCount: 2,
					},
				},
			},
		},
	}

	result, err := checker.CheckECS(context.Background(), "bedrock-chat-dev", "bedrock-chat-dev-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Healthy {
		t.Error("expected Healthy=true")
	}
	if result.RunningCount != 2 {
		t.Errorf("expected RunningCount=2, got %d", result.RunningCount)
	}
}

func TestCheckECS_RunningLessThanDesired(t *testing.T) {
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						Status:       aws.String("ACTIVE"),
						RunningCount: 0,
						DesiredCount: 2,
					},
				},
			},
		},
	}

	result, err := checker.CheckECS(context.Background(), "bedrock-chat-dev", "bedrock-chat-dev-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Healthy {
		t.Error("expected Healthy=false when running < desired")
	}
}

func TestCheckECS_InactiveStatus(t *testing.T) {
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						Status:       aws.String("INACTIVE"),
						RunningCount: 0,
						DesiredCount: 0,
					},
				},
			},
		},
	}

	result, err := checker.CheckECS(context.Background(), "bedrock-chat-dev", "bedrock-chat-dev-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Healthy {
		t.Error("expected Healthy=false for INACTIVE service")
	}
}

func TestCheckECS_NotFound(t *testing.T) {
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{Services: []ecstypes.Service{}},
		},
	}

	result, err := checker.CheckECS(context.Background(), "bedrock-chat-dev", "non-existent-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Healthy {
		t.Error("expected Healthy=false for not-found service")
	}
	if result.Status != "not-found" {
		t.Errorf("expected status=not-found, got %s", result.Status)
	}
}

func TestCheckECS_EmptyParams(t *testing.T) {
	checker := &Checker{ecsCli: &mockECSClient{}}
	result, err := checker.CheckECS(context.Background(), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for empty params")
	}
}

func TestCheckECS_APIError(t *testing.T) {
	checker := &Checker{
		ecsCli: &mockECSClient{err: fmt.Errorf("API error")},
	}
	_, err := checker.CheckECS(context.Background(), "cluster", "service")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// ── CheckALB ──────────────────────────────────────────────────────────────────

func TestCheckALB_AllHealthy(t *testing.T) {
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target: &elbv2types.TargetDescription{
							Id:   aws.String("10.0.1.100"),
							Port: aws.Int32(8501),
						},
						TargetHealth: &elbv2types.TargetHealth{
							State: elbv2types.TargetHealthStateEnumHealthy,
						},
					},
				},
			},
		},
	}

	result, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllHealthy {
		t.Error("expected AllHealthy=true")
	}
	if len(result.Targets) != 1 {
		t.Errorf("expected 1 target, got %d", len(result.Targets))
	}
}

func TestCheckALB_Unhealthy(t *testing.T) {
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target: &elbv2types.TargetDescription{
							Id:   aws.String("10.0.1.101"),
							Port: aws.Int32(8501),
						},
						TargetHealth: &elbv2types.TargetHealth{
							State:  elbv2types.TargetHealthStateEnumUnhealthy,
							Reason: elbv2types.TargetHealthReasonEnumFailedHealthChecks,
						},
					},
				},
			},
		},
	}

	result, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AllHealthy {
		t.Error("expected AllHealthy=false")
	}
}

func TestCheckALB_EmptyARN(t *testing.T) {
	checker := &Checker{elbv2: &mockELBV2Client{}}
	result, err := checker.CheckALB(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for empty ARN")
	}
}

func TestCheckALB_APIError(t *testing.T) {
	checker := &Checker{
		elbv2: &mockELBV2Client{err: fmt.Errorf("API error")},
	}
	_, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// ── CheckDynamoDB ─────────────────────────────────────────────────────────────

func TestCheckDynamoDB_Active(t *testing.T) {
	checker := &Checker{
		dynoCli: &mockDynamoDBClient{
			output: &dynamodb.DescribeTableOutput{
				Table: &dynamodbtypes.TableDescription{
					TableName:   aws.String("bedrock-chat-dev-sessions"),
					TableStatus: dynamodbtypes.TableStatusActive,
				},
			},
		},
	}

	result, err := checker.CheckDynamoDB(context.Background(), "bedrock-chat-dev-sessions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Healthy {
		t.Error("expected Healthy=true for ACTIVE table")
	}
	if result.Status != string(dynamodbtypes.TableStatusActive) {
		t.Errorf("unexpected status: %s", result.Status)
	}
}

func TestCheckDynamoDB_Creating(t *testing.T) {
	checker := &Checker{
		dynoCli: &mockDynamoDBClient{
			output: &dynamodb.DescribeTableOutput{
				Table: &dynamodbtypes.TableDescription{
					TableName:   aws.String("bedrock-chat-dev-sessions"),
					TableStatus: dynamodbtypes.TableStatusCreating,
				},
			},
		},
	}

	result, err := checker.CheckDynamoDB(context.Background(), "bedrock-chat-dev-sessions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Healthy {
		t.Error("expected Healthy=false for CREATING table")
	}
}

func TestCheckDynamoDB_EmptyTableName(t *testing.T) {
	checker := &Checker{dynoCli: &mockDynamoDBClient{}}
	result, err := checker.CheckDynamoDB(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for empty table name")
	}
}

func TestCheckDynamoDB_APIError(t *testing.T) {
	checker := &Checker{
		dynoCli: &mockDynamoDBClient{err: fmt.Errorf("API error")},
	}
	_, err := checker.CheckDynamoDB(context.Background(), "bedrock-chat-dev-sessions")
	if err == nil {
		t.Error("expected error, got nil")
	}
}
