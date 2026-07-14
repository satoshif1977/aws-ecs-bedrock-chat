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

// ── CheckECS 追加ケース ───────────────────────────────────────

func TestCheckECS_DesiredZero(t *testing.T) {
	// DesiredCount=0 のときは ACTIVE でも Healthy=false になること
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 0, DesiredCount: 0},
				},
			},
		},
	}
	result, err := checker.CheckECS(context.Background(), "cluster", "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Healthy {
		t.Error("expected Healthy=false when DesiredCount=0")
	}
}

func TestCheckECS_ClusterAndServiceNamePreserved(t *testing.T) {
	// ClusterName / ServiceName がレスポンスに保持されること
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 1, DesiredCount: 1},
				},
			},
		},
	}
	result, err := checker.CheckECS(context.Background(), "my-cluster", "my-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ClusterName != "my-cluster" {
		t.Errorf("ClusterName: got %q, want my-cluster", result.ClusterName)
	}
	if result.ServiceName != "my-service" {
		t.Errorf("ServiceName: got %q, want my-service", result.ServiceName)
	}
}

func TestCheckECS_OnlyClusterEmpty(t *testing.T) {
	// clusterName が空なら serviceName があっても nil を返すこと
	checker := &Checker{ecsCli: &mockECSClient{}}
	result, err := checker.CheckECS(context.Background(), "", "some-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil when clusterName is empty")
	}
}

func TestCheckECS_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		running     int32
		desired     int32
		wantHealthy bool
	}{
		{"ACTIVE running=desired", "ACTIVE", 2, 2, true},
		{"ACTIVE running<desired", "ACTIVE", 0, 2, false},
		{"INACTIVE desired=0", "INACTIVE", 0, 0, false},
		{"ACTIVE desired=0", "ACTIVE", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &Checker{
				ecsCli: &mockECSClient{
					output: &ecs.DescribeServicesOutput{
						Services: []ecstypes.Service{
							{Status: aws.String(tt.status), RunningCount: tt.running, DesiredCount: tt.desired},
						},
					},
				},
			}
			result, err := checker.CheckECS(context.Background(), "cluster", "service")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Healthy != tt.wantHealthy {
				t.Errorf("Healthy: got %v, want %v", result.Healthy, tt.wantHealthy)
			}
		})
	}
}

// ── CheckALB 追加ケース ───────────────────────────────────────

func TestCheckALB_EmptyTargets(t *testing.T) {
	// ターゲット0件のときは AllHealthy=true（デフォルト）であること
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{},
			},
		},
	}
	result, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllHealthy {
		t.Error("expected AllHealthy=true for empty targets")
	}
	if len(result.Targets) != 0 {
		t.Errorf("expected 0 targets, got %d", len(result.Targets))
	}
}

func TestCheckALB_MixedHealth(t *testing.T) {
	// 健全1台・不健全1台が混在する場合は AllHealthy=false になること
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.1.1"), Port: aws.Int32(8080)},
						TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
					},
					{
						Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.1.2"), Port: aws.Int32(8080)},
						TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumUnhealthy},
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
		t.Error("expected AllHealthy=false for mixed health targets")
	}
	if len(result.Targets) != 2 {
		t.Errorf("expected 2 targets, got %d", len(result.Targets))
	}
}

func TestCheckALB_TargetGroupARNPreserved(t *testing.T) {
	const wantARN = "arn:aws:elasticloadbalancing:ap-northeast-1:999999999999:targetgroup/my-tg/xyz"
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{},
		},
	}
	result, err := checker.CheckALB(context.Background(), wantARN)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TargetGroupARN != wantARN {
		t.Errorf("TargetGroupARN: got %q, want %q", result.TargetGroupARN, wantARN)
	}
}

// ── CheckDynamoDB 追加ケース ──────────────────────────────────

func TestCheckDynamoDB_Deleting(t *testing.T) {
	checker := &Checker{
		dynoCli: &mockDynamoDBClient{
			output: &dynamodb.DescribeTableOutput{
				Table: &dynamodbtypes.TableDescription{TableStatus: dynamodbtypes.TableStatusDeleting},
			},
		},
	}
	result, err := checker.CheckDynamoDB(context.Background(), "test-table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Healthy {
		t.Error("expected Healthy=false for DELETING table")
	}
}

func TestCheckDynamoDB_TableNamePreserved(t *testing.T) {
	checker := &Checker{
		dynoCli: &mockDynamoDBClient{
			output: &dynamodb.DescribeTableOutput{
				Table: &dynamodbtypes.TableDescription{TableStatus: dynamodbtypes.TableStatusActive},
			},
		},
	}
	result, err := checker.CheckDynamoDB(context.Background(), "my-sessions-table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TableName != "my-sessions-table" {
		t.Errorf("TableName: got %q, want my-sessions-table", result.TableName)
	}
}

func TestCheckDynamoDB_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		status      dynamodbtypes.TableStatus
		wantHealthy bool
	}{
		{"ACTIVE", dynamodbtypes.TableStatusActive, true},
		{"CREATING", dynamodbtypes.TableStatusCreating, false},
		{"DELETING", dynamodbtypes.TableStatusDeleting, false},
		{"UPDATING", dynamodbtypes.TableStatusUpdating, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &Checker{
				dynoCli: &mockDynamoDBClient{
					output: &dynamodb.DescribeTableOutput{
						Table: &dynamodbtypes.TableDescription{TableStatus: tt.status},
					},
				},
			}
			result, err := checker.CheckDynamoDB(context.Background(), "test-table")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Healthy != tt.wantHealthy {
				t.Errorf("status=%s: Healthy: got %v, want %v", tt.status, result.Healthy, tt.wantHealthy)
			}
		})
	}
}

// ── CheckECS 追加ケース（挙動文書化） ────────────────────────────────────────

func TestCheckECS_RunningExceedsDesired(t *testing.T) {
	// Running=3 > Desired=2 のときも ACTIVE かつ Desired>0 なら Healthy=true になること
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 3, DesiredCount: 2},
				},
			},
		},
	}
	result, err := checker.CheckECS(context.Background(), "cluster", "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Healthy {
		t.Error("expected Healthy=true when running > desired and ACTIVE")
	}
}

func TestCheckECS_OnlyServiceEmpty(t *testing.T) {
	// serviceName のみ空のときも nil を返すこと（OnlyClusterEmpty の対称）
	checker := &Checker{ecsCli: &mockECSClient{}}
	result, err := checker.CheckECS(context.Background(), "some-cluster", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil when serviceName is empty")
	}
}

func TestCheckECS_MultipleServicesUsesFirst(t *testing.T) {
	// DescribeServices が複数サービスを返した場合、先頭（[0]）を使うこと
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 2, DesiredCount: 2},
					{Status: aws.String("INACTIVE"), RunningCount: 0, DesiredCount: 0},
				},
			},
		},
	}
	result, err := checker.CheckECS(context.Background(), "cluster", "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Healthy {
		t.Error("expected Healthy=true: first service (ACTIVE) should be used")
	}
}

// ── CheckALB 追加ケース（挙動文書化） ────────────────────────────────────────

func TestCheckALB_MultipleAllHealthy(t *testing.T) {
	// 3台全て healthy のとき AllHealthy=true・Targets 件数=3 になること
	makeTarget := func(ip string) elbv2types.TargetHealthDescription {
		return elbv2types.TargetHealthDescription{
			Target:       &elbv2types.TargetDescription{Id: aws.String(ip), Port: aws.Int32(8080)},
			TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
		}
	}
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					makeTarget("10.0.1.1"),
					makeTarget("10.0.1.2"),
					makeTarget("10.0.1.3"),
				},
			},
		},
	}
	result, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AllHealthy {
		t.Error("expected AllHealthy=true for 3 healthy targets")
	}
	if len(result.Targets) != 3 {
		t.Errorf("expected 3 targets, got %d", len(result.Targets))
	}
}
