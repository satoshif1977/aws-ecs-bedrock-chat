package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// ── キャプチャモック（API 入力パラメータ検証用） ──────────────────

type captureECSClient struct {
	mockECSClient
	inputs []*ecs.DescribeServicesInput
}

func (c *captureECSClient) DescribeServices(ctx context.Context, input *ecs.DescribeServicesInput, opts ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	c.inputs = append(c.inputs, input)
	return c.mockECSClient.DescribeServices(ctx, input, opts...)
}

type captureELBV2Client struct {
	mockELBV2Client
	inputs []*elasticloadbalancingv2.DescribeTargetHealthInput
}

func (c *captureELBV2Client) DescribeTargetHealth(ctx context.Context, input *elasticloadbalancingv2.DescribeTargetHealthInput, opts ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTargetHealthOutput, error) {
	c.inputs = append(c.inputs, input)
	return c.mockELBV2Client.DescribeTargetHealth(ctx, input, opts...)
}

type captureDynamoDBClient struct {
	mockDynamoDBClient
	inputs []*dynamodb.DescribeTableInput
}

func (c *captureDynamoDBClient) DescribeTable(ctx context.Context, input *dynamodb.DescribeTableInput, opts ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	c.inputs = append(c.inputs, input)
	return c.mockDynamoDBClient.DescribeTable(ctx, input, opts...)
}

// ── キャプチャモック: API 入力パラメータ検証 ──────────────────────

func TestCapture_ECSClusterAndService(t *testing.T) {
	cap := &captureECSClient{
		mockECSClient: mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 1, DesiredCount: 1},
				},
			},
		},
	}
	checker := &Checker{ecsCli: cap}
	checker.CheckECS(context.Background(), "prod-cluster", "api-service")

	if len(cap.inputs) != 1 {
		t.Fatalf("DescribeServices calls = %d, want 1", len(cap.inputs))
	}
	if *cap.inputs[0].Cluster != "prod-cluster" {
		t.Errorf("Cluster = %q, want prod-cluster", *cap.inputs[0].Cluster)
	}
	if len(cap.inputs[0].Services) != 1 || cap.inputs[0].Services[0] != "api-service" {
		t.Errorf("Services = %v, want [api-service]", cap.inputs[0].Services)
	}
}

func TestCapture_ALBTargetGroupARN(t *testing.T) {
	const arn = "arn:aws:elasticloadbalancing:ap-northeast-1:123:targetgroup/tg/abc"
	cap := &captureELBV2Client{
		mockELBV2Client: mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{},
		},
	}
	checker := &Checker{elbv2: cap}
	checker.CheckALB(context.Background(), arn)

	if len(cap.inputs) != 1 {
		t.Fatalf("DescribeTargetHealth calls = %d, want 1", len(cap.inputs))
	}
	if *cap.inputs[0].TargetGroupArn != arn {
		t.Errorf("TargetGroupArn = %q, want %q", *cap.inputs[0].TargetGroupArn, arn)
	}
}

func TestCapture_DynamoDBTableName(t *testing.T) {
	cap := &captureDynamoDBClient{
		mockDynamoDBClient: mockDynamoDBClient{
			output: &dynamodb.DescribeTableOutput{
				Table: &dynamodbtypes.TableDescription{
					TableStatus: dynamodbtypes.TableStatusActive,
				},
			},
		},
	}
	checker := &Checker{dynoCli: cap}
	checker.CheckDynamoDB(context.Background(), "sessions-table")

	if len(cap.inputs) != 1 {
		t.Fatalf("DescribeTable calls = %d, want 1", len(cap.inputs))
	}
	if *cap.inputs[0].TableName != "sessions-table" {
		t.Errorf("TableName = %q, want sessions-table", *cap.inputs[0].TableName)
	}
}

func TestCapture_EmptyParamsNoAPICalls(t *testing.T) {
	ecsCap := &captureECSClient{mockECSClient: mockECSClient{}}
	elbCap := &captureELBV2Client{mockELBV2Client: mockELBV2Client{}}
	dynoCap := &captureDynamoDBClient{mockDynamoDBClient: mockDynamoDBClient{}}

	checker := &Checker{ecsCli: ecsCap, elbv2: elbCap, dynoCli: dynoCap}

	checker.CheckECS(context.Background(), "", "")
	checker.CheckALB(context.Background(), "")
	checker.CheckDynamoDB(context.Background(), "")

	if len(ecsCap.inputs) != 0 {
		t.Errorf("ECS API should not be called with empty params, got %d calls", len(ecsCap.inputs))
	}
	if len(elbCap.inputs) != 0 {
		t.Errorf("ELBV2 API should not be called with empty ARN, got %d calls", len(elbCap.inputs))
	}
	if len(dynoCap.inputs) != 0 {
		t.Errorf("DynamoDB API should not be called with empty table, got %d calls", len(dynoCap.inputs))
	}
}

// ── errors.Is によるエラーラップ検証 ──────────────────────────────

func TestCheckECS_ErrorWrapsOriginal(t *testing.T) {
	originalErr := errors.New("ECS network timeout")
	checker := &Checker{ecsCli: &mockECSClient{err: originalErr}}
	_, err := checker.CheckECS(context.Background(), "cluster", "service")
	if !errors.Is(err, originalErr) {
		t.Errorf("error should wrap original: got %v", err)
	}
	if !strings.Contains(err.Error(), "ECS DescribeServices") {
		t.Errorf("error should contain prefix: %v", err)
	}
}

func TestCheckALB_ErrorWrapsOriginal(t *testing.T) {
	originalErr := errors.New("ALB throttled")
	checker := &Checker{elbv2: &mockELBV2Client{err: originalErr}}
	_, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123:targetgroup/tg/abc")
	if !errors.Is(err, originalErr) {
		t.Errorf("error should wrap original: got %v", err)
	}
	if !strings.Contains(err.Error(), "ALB DescribeTargetHealth") {
		t.Errorf("error should contain prefix: %v", err)
	}
}

func TestCheckDynamoDB_ErrorWrapsOriginal(t *testing.T) {
	originalErr := errors.New("DynamoDB access denied")
	checker := &Checker{dynoCli: &mockDynamoDBClient{err: originalErr}}
	_, err := checker.CheckDynamoDB(context.Background(), "my-table")
	if !errors.Is(err, originalErr) {
		t.Errorf("error should wrap original: got %v", err)
	}
	if !strings.Contains(err.Error(), "DynamoDB DescribeTable") {
		t.Errorf("error should contain prefix: %v", err)
	}
}

// ── JSON シリアライズ検証 ─────────────────────────────────────────

func TestECSHealth_JSONFields(t *testing.T) {
	h := ECSHealth{
		ClusterName:  "prod",
		ServiceName:  "api",
		Status:       "ACTIVE",
		RunningCount: 3,
		DesiredCount: 3,
		Healthy:      true,
	}
	b, _ := json.Marshal(h)
	var m map[string]any
	json.Unmarshal(b, &m)

	required := []string{"cluster_name", "service_name", "status", "running_count", "desired_count", "healthy"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON missing field %q", key)
		}
	}
}

func TestALBHealth_JSONFields(t *testing.T) {
	h := ALBHealth{
		TargetGroupARN: "arn:test",
		Targets:        []TargetStatus{{ID: "10.0.0.1", Port: 8080, State: "healthy"}},
		AllHealthy:     true,
	}
	b, _ := json.Marshal(h)
	var m map[string]any
	json.Unmarshal(b, &m)

	required := []string{"target_group_arn", "targets", "all_healthy"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON missing field %q", key)
		}
	}
}

func TestTargetStatus_JSONFields(t *testing.T) {
	ts := TargetStatus{ID: "10.0.0.1", Port: 8080, State: "healthy", Reason: "OK"}
	b, _ := json.Marshal(ts)
	var m map[string]any
	json.Unmarshal(b, &m)

	required := []string{"id", "port", "state", "reason"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON missing field %q", key)
		}
	}
}

func TestTargetStatus_ReasonOmitEmpty(t *testing.T) {
	ts := TargetStatus{ID: "10.0.0.1", Port: 8080, State: "healthy"}
	b, _ := json.Marshal(ts)
	if strings.Contains(string(b), "reason") {
		t.Errorf("reason should be omitted when empty, got: %s", string(b))
	}
}

func TestDynamoDBHealth_JSONFields(t *testing.T) {
	h := DynamoDBHealth{TableName: "sessions", Status: "ACTIVE", Healthy: true}
	b, _ := json.Marshal(h)
	var m map[string]any
	json.Unmarshal(b, &m)

	required := []string{"table_name", "status", "healthy"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON missing field %q", key)
		}
	}
}

func TestHealthSummary_JSONFields(t *testing.T) {
	s := HealthSummary{
		ECS:      &ECSHealth{Healthy: true},
		ALB:      &ALBHealth{AllHealthy: true},
		DynamoDB: &DynamoDBHealth{Healthy: true},
		Healthy:  true,
	}
	b, _ := json.Marshal(s)
	var m map[string]any
	json.Unmarshal(b, &m)

	required := []string{"ecs", "alb", "dynamodb", "healthy"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON missing field %q", key)
		}
	}
}

func TestHealthSummary_OmitEmptyNil(t *testing.T) {
	s := HealthSummary{Healthy: true}
	b, _ := json.Marshal(s)
	str := string(b)
	for _, key := range []string{"ecs", "alb", "dynamodb"} {
		if strings.Contains(str, fmt.Sprintf("%q:", key)) {
			t.Errorf("nil %s should be omitted, got: %s", key, str)
		}
	}
}

// ── CheckECS ヘルシー条件 テーブル駆動（拡張版） ─────────────────

func TestCheckECS_HealthyCondition_Extended(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		running     int32
		desired     int32
		wantHealthy bool
	}{
		{"ACTIVE 1/1", "ACTIVE", 1, 1, true},
		{"ACTIVE 2/2", "ACTIVE", 2, 2, true},
		{"ACTIVE 10/10", "ACTIVE", 10, 10, true},
		{"ACTIVE 3/2 (over)", "ACTIVE", 3, 2, true},
		{"ACTIVE 0/2 (under)", "ACTIVE", 0, 2, false},
		{"ACTIVE 1/2 (under)", "ACTIVE", 1, 2, false},
		{"ACTIVE 0/0", "ACTIVE", 0, 0, false},
		{"INACTIVE 0/0", "INACTIVE", 0, 0, false},
		{"DRAINING 2/2", "DRAINING", 2, 2, false},
		{"PRIMARY 1/1", "PRIMARY", 1, 1, false},
		{"empty status 1/1", "", 1, 1, false},
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
			result, err := checker.CheckECS(context.Background(), "c", "s")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Healthy != tt.wantHealthy {
				t.Errorf("Healthy = %v, want %v", result.Healthy, tt.wantHealthy)
			}
		})
	}
}

// ── CheckALB ターゲット状態 テーブル駆動 ─────────────────────────

func TestCheckALB_TargetStates_Table(t *testing.T) {
	tests := []struct {
		name        string
		state       elbv2types.TargetHealthStateEnum
		wantHealthy bool
	}{
		{"healthy", elbv2types.TargetHealthStateEnumHealthy, true},
		{"unhealthy", elbv2types.TargetHealthStateEnumUnhealthy, false},
		{"draining", elbv2types.TargetHealthStateEnumDraining, false},
		{"initial", elbv2types.TargetHealthStateEnumInitial, false},
		{"unused", elbv2types.TargetHealthStateEnumUnused, false},
		{"unavailable", elbv2types.TargetHealthStateEnumUnavailable, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &Checker{
				elbv2: &mockELBV2Client{
					output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
						TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
							{
								Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.0.1"), Port: aws.Int32(80)},
								TargetHealth: &elbv2types.TargetHealth{State: tt.state},
							},
						},
					},
				},
			}
			result, err := checker.CheckALB(context.Background(), "arn:test")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.AllHealthy != tt.wantHealthy {
				t.Errorf("AllHealthy = %v, want %v", result.AllHealthy, tt.wantHealthy)
			}
		})
	}
}

// ── DynamoDB テーブルステータス テーブル駆動（拡張版） ─────────────

func TestCheckDynamoDB_AllStatuses_Table(t *testing.T) {
	tests := []struct {
		status      dynamodbtypes.TableStatus
		wantHealthy bool
	}{
		{dynamodbtypes.TableStatusActive, true},
		{dynamodbtypes.TableStatusCreating, false},
		{dynamodbtypes.TableStatusDeleting, false},
		{dynamodbtypes.TableStatusUpdating, false},
		{dynamodbtypes.TableStatusArchiving, false},
		{dynamodbtypes.TableStatusArchived, false},
		{dynamodbtypes.TableStatusInaccessibleEncryptionCredentials, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
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
				t.Errorf("status=%s: Healthy = %v, want %v", tt.status, result.Healthy, tt.wantHealthy)
			}
		})
	}
}

// ── HealthSummary 統合テスト ──────────────────────────────────────

func TestHealthSummary_AllHealthy(t *testing.T) {
	summary := HealthSummary{
		ECS:      &ECSHealth{Healthy: true},
		ALB:      &ALBHealth{AllHealthy: true},
		DynamoDB: &DynamoDBHealth{Healthy: true},
		Healthy:  true,
	}
	if !summary.Healthy {
		t.Error("all components healthy: summary should be healthy")
	}
}

func TestHealthSummary_ECSUnhealthy(t *testing.T) {
	summary := HealthSummary{
		ECS:      &ECSHealth{Healthy: false},
		ALB:      &ALBHealth{AllHealthy: true},
		DynamoDB: &DynamoDBHealth{Healthy: true},
		Healthy:  false,
	}
	if summary.Healthy {
		t.Error("ECS unhealthy: summary should not be healthy")
	}
}

func TestHealthSummary_ALBUnhealthy(t *testing.T) {
	summary := HealthSummary{
		ECS:      &ECSHealth{Healthy: true},
		ALB:      &ALBHealth{AllHealthy: false},
		DynamoDB: &DynamoDBHealth{Healthy: true},
		Healthy:  false,
	}
	if summary.Healthy {
		t.Error("ALB unhealthy: summary should not be healthy")
	}
}

func TestHealthSummary_DynamoDBUnhealthy(t *testing.T) {
	summary := HealthSummary{
		ECS:      &ECSHealth{Healthy: true},
		ALB:      &ALBHealth{AllHealthy: true},
		DynamoDB: &DynamoDBHealth{Healthy: false},
		Healthy:  false,
	}
	if summary.Healthy {
		t.Error("DynamoDB unhealthy: summary should not be healthy")
	}
}

func TestHealthSummary_AllUnhealthy(t *testing.T) {
	summary := HealthSummary{
		ECS:      &ECSHealth{Healthy: false},
		ALB:      &ALBHealth{AllHealthy: false},
		DynamoDB: &DynamoDBHealth{Healthy: false},
		Healthy:  false,
	}
	if summary.Healthy {
		t.Error("all unhealthy: summary should not be healthy")
	}
}

func TestHealthSummary_NilComponents(t *testing.T) {
	summary := HealthSummary{Healthy: true}
	if !summary.Healthy {
		t.Error("nil components should not force unhealthy")
	}
	if summary.ECS != nil || summary.ALB != nil || summary.DynamoDB != nil {
		t.Error("components should be nil")
	}
}

// ── CheckECS not-found フィールド詳細検証 ────────────────────────

func TestCheckECS_NotFoundFields(t *testing.T) {
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{Services: []ecstypes.Service{}},
		},
	}
	result, _ := checker.CheckECS(context.Background(), "my-cluster", "missing-svc")

	if result.ClusterName != "my-cluster" {
		t.Errorf("ClusterName = %q, want my-cluster", result.ClusterName)
	}
	if result.ServiceName != "missing-svc" {
		t.Errorf("ServiceName = %q, want missing-svc", result.ServiceName)
	}
	if result.Status != "not-found" {
		t.Errorf("Status = %q, want not-found", result.Status)
	}
	if result.Healthy {
		t.Error("not-found service should not be healthy")
	}
	if result.RunningCount != 0 {
		t.Errorf("RunningCount = %d, want 0", result.RunningCount)
	}
	if result.DesiredCount != 0 {
		t.Errorf("DesiredCount = %d, want 0", result.DesiredCount)
	}
}

// ── ALB Targets 詳細検証 ─────────────────────────────────────────

func TestCheckALB_TargetsDetailPreserved(t *testing.T) {
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target: &elbv2types.TargetDescription{
							Id: aws.String("10.0.1.100"), Port: aws.Int32(8501),
						},
						TargetHealth: &elbv2types.TargetHealth{
							State:  elbv2types.TargetHealthStateEnumUnhealthy,
							Reason: elbv2types.TargetHealthReasonEnumFailedHealthChecks,
						},
					},
					{
						Target: &elbv2types.TargetDescription{
							Id: aws.String("10.0.2.200"), Port: aws.Int32(8502),
						},
						TargetHealth: &elbv2types.TargetHealth{
							State: elbv2types.TargetHealthStateEnumHealthy,
						},
					},
				},
			},
		},
	}

	result, err := checker.CheckALB(context.Background(), "arn:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(result.Targets))
	}

	// First target
	if result.Targets[0].ID != "10.0.1.100" {
		t.Errorf("target[0].ID = %q", result.Targets[0].ID)
	}
	if result.Targets[0].Port != 8501 {
		t.Errorf("target[0].Port = %d", result.Targets[0].Port)
	}
	if result.Targets[0].State != "unhealthy" {
		t.Errorf("target[0].State = %q", result.Targets[0].State)
	}

	// Second target
	if result.Targets[1].ID != "10.0.2.200" {
		t.Errorf("target[1].ID = %q", result.Targets[1].ID)
	}
	if result.Targets[1].Port != 8502 {
		t.Errorf("target[1].Port = %d", result.Targets[1].Port)
	}
	if result.Targets[1].State != "healthy" {
		t.Errorf("target[1].State = %q", result.Targets[1].State)
	}
}

// ── ベンチマーク ──────────────────────────────────────────────────

func BenchmarkCheckECS_Healthy(b *testing.B) {
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 2, DesiredCount: 2},
				},
			},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.CheckECS(context.Background(), "cluster", "service")
	}
}

func BenchmarkCheckALB_TwoTargets(b *testing.B) {
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.0.1"), Port: aws.Int32(80)},
						TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
					},
					{
						Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.0.2"), Port: aws.Int32(80)},
						TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
					},
				},
			},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.CheckALB(context.Background(), "arn:test")
	}
}

func BenchmarkCheckDynamoDB_Active(b *testing.B) {
	checker := &Checker{
		dynoCli: &mockDynamoDBClient{
			output: &dynamodb.DescribeTableOutput{
				Table: &dynamodbtypes.TableDescription{TableStatus: dynamodbtypes.TableStatusActive},
			},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.CheckDynamoDB(context.Background(), "sessions")
	}
}

func BenchmarkCheckECS_EmptyParams(b *testing.B) {
	checker := &Checker{ecsCli: &mockECSClient{}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checker.CheckECS(context.Background(), "", "")
	}
}
