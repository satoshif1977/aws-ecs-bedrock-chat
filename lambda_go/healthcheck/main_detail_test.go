package main

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// ── CheckALB 追加テスト ───────────────────────────────────────

func TestCheckALB_TargetWithNilTarget(t *testing.T) {
	// thd.Target が nil でも panic しないこと（コードの nil ガード確認）
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target:       nil, // Target 自体が nil
						TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
					},
				},
			},
		},
	}
	result, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Targets) != 1 {
		t.Errorf("expected 1 target, got %d", len(result.Targets))
	}
	// nil Target の ID・Port はゼロ値になること
	if result.Targets[0].ID != "" {
		t.Errorf("ID: want empty string for nil Target, got %q", result.Targets[0].ID)
	}
	if result.Targets[0].Port != 0 {
		t.Errorf("Port: want 0 for nil Target, got %d", result.Targets[0].Port)
	}
}

func TestCheckALB_TargetIDPreserved(t *testing.T) {
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target:       &elbv2types.TargetDescription{Id: aws.String("192.168.1.50"), Port: aws.Int32(8080)},
						TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
					},
				},
			},
		},
	}
	result, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Targets[0].ID != "192.168.1.50" {
		t.Errorf("ID: want 192.168.1.50, got %q", result.Targets[0].ID)
	}
}

func TestCheckALB_TargetPortPreserved(t *testing.T) {
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.0.1"), Port: aws.Int32(3000)},
						TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
					},
				},
			},
		},
	}
	result, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Targets[0].Port != 3000 {
		t.Errorf("Port: want 3000, got %d", result.Targets[0].Port)
	}
}

func TestCheckALB_DrainState(t *testing.T) {
	// DRAINING 状態のターゲットは AllHealthy=false になること
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.0.1"), Port: aws.Int32(8080)},
						TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumDraining},
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
		t.Error("expected AllHealthy=false for DRAINING target")
	}
}

func TestCheckALB_ReasonPreserved(t *testing.T) {
	// Reason フィールドが TargetStatus.Reason に保持されること
	checker := &Checker{
		elbv2: &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{
						Target: &elbv2types.TargetDescription{Id: aws.String("10.0.0.1"), Port: aws.Int32(8080)},
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
	if result.Targets[0].Reason != string(elbv2types.TargetHealthReasonEnumFailedHealthChecks) {
		t.Errorf("Reason: want %q, got %q",
			string(elbv2types.TargetHealthReasonEnumFailedHealthChecks),
			result.Targets[0].Reason)
	}
}

// ── CheckECS フィールド保持テスト ─────────────────────────────

func TestCheckECS_StatusFieldSet(t *testing.T) {
	// result.Status が API から取得した Status と一致すること
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 1, DesiredCount: 1},
				},
			},
		},
	}
	result, err := checker.CheckECS(context.Background(), "cluster", "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ACTIVE" {
		t.Errorf("Status: want ACTIVE, got %q", result.Status)
	}
}

func TestCheckECS_RunningCountField(t *testing.T) {
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 5, DesiredCount: 5},
				},
			},
		},
	}
	result, err := checker.CheckECS(context.Background(), "cluster", "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RunningCount != 5 {
		t.Errorf("RunningCount: want 5, got %d", result.RunningCount)
	}
}

func TestCheckECS_DesiredCountField(t *testing.T) {
	checker := &Checker{
		ecsCli: &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{Status: aws.String("ACTIVE"), RunningCount: 3, DesiredCount: 3},
				},
			},
		},
	}
	result, err := checker.CheckECS(context.Background(), "cluster", "service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DesiredCount != 3 {
		t.Errorf("DesiredCount: want 3, got %d", result.DesiredCount)
	}
}

// ── CheckDynamoDB フィールド保持テスト ────────────────────────

func TestCheckDynamoDB_StatusField(t *testing.T) {
	// result.Status が API から取得した TableStatus 文字列と一致すること
	checker := &Checker{
		dynoCli: &mockDynamoDBClient{
			output: &dynamodb.DescribeTableOutput{
				Table: &dynamodbtypes.TableDescription{
					TableStatus: dynamodbtypes.TableStatusActive,
				},
			},
		},
	}
	result, err := checker.CheckDynamoDB(context.Background(), "my-table")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != string(dynamodbtypes.TableStatusActive) {
		t.Errorf("Status: want %q, got %q", string(dynamodbtypes.TableStatusActive), result.Status)
	}
}

// ── Fuzz テスト ───────────────────────────────────────────────

// FuzzCheckALBStatesNoPanic は様々な TargetHealthState で
// CheckALB が panic しないことを検証する。
func FuzzCheckALBStatesNoPanic(f *testing.F) {
	states := []elbv2types.TargetHealthStateEnum{
		elbv2types.TargetHealthStateEnumHealthy,
		elbv2types.TargetHealthStateEnumUnhealthy,
		elbv2types.TargetHealthStateEnumDraining,
		elbv2types.TargetHealthStateEnumInitial,
		elbv2types.TargetHealthStateEnumUnused,
	}
	for _, s := range states {
		f.Add(string(s))
	}
	f.Fuzz(func(t *testing.T, stateStr string) {
		if !utf8.ValidString(stateStr) {
			t.Skip()
		}
		checker := &Checker{
			elbv2: &mockELBV2Client{
				output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
					TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
						{
							Target:       &elbv2types.TargetDescription{Id: aws.String("10.0.0.1"), Port: aws.Int32(8080)},
							TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnum(stateStr)},
						},
					},
				},
			},
		}
		result, err := checker.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/test/abc")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		// healthy 以外は AllHealthy=false になること
		if stateStr == string(elbv2types.TargetHealthStateEnumHealthy) {
			if !result.AllHealthy {
				t.Errorf("state=healthy: expected AllHealthy=true")
			}
		} else {
			if result.AllHealthy {
				t.Errorf("state=%q: expected AllHealthy=false", stateStr)
			}
		}
	})
}
