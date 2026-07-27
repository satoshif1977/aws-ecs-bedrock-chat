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

// ── FuzzCheckECS ──────────────────────────────────────────────────────────────

// FuzzCheckECSEmptyReturnsNil は clusterName または serviceName が空のとき
// 常に nil を返すことを検証する。
// 不変条件: 空パラメーターでは AWS API を呼ばず nil を返す（コスト・エラー防止）。
func FuzzCheckECSEmptyReturnsNil(f *testing.F) {
	f.Add("", "my-service")
	f.Add("my-cluster", "")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, cluster, service string) {
		if !utf8.ValidString(cluster) || !utf8.ValidString(service) {
			t.Skip()
		}
		if cluster != "" && service != "" {
			t.Skip() // 両方非空はこの関数のスコープ外
		}
		checker := &Checker{ecsCli: &mockECSClient{}}
		result, err := checker.CheckECS(context.Background(), cluster, service)
		if err != nil {
			t.Errorf("CheckECS(%q, %q): 空パラメーターでエラーが返った: %v", cluster, service, err)
		}
		if result != nil {
			t.Errorf("CheckECS(%q, %q): 空パラメーターで nil 以外が返った: %+v", cluster, service, result)
		}
	})
}

// FuzzCheckECSHealthyCondition は ACTIVE ステータスで running >= desired > 0 のとき
// 常に Healthy==true を返すことを検証する。
// 不変条件: ヘルシー判定は Status・RunningCount・DesiredCount の3条件の論理積。
func FuzzCheckECSHealthyCondition(f *testing.F) {
	f.Add("my-cluster", "my-service", int32(2), int32(2))
	f.Add("prod-cluster", "api-service", int32(10), int32(5))
	f.Add("cluster", "svc", int32(1), int32(1))

	f.Fuzz(func(t *testing.T, cluster, service string, desired, running int32) {
		if !utf8.ValidString(cluster) || !utf8.ValidString(service) {
			t.Skip()
		}
		if cluster == "" || service == "" {
			t.Skip()
		}
		if desired <= 0 || running < desired {
			t.Skip() // ヘルシーでないケースはこの関数のスコープ外
		}

		mock := &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						Status:       aws.String("ACTIVE"),
						RunningCount: running,
						DesiredCount: desired,
					},
				},
			},
		}
		checker := &Checker{ecsCli: mock}
		result, err := checker.CheckECS(context.Background(), cluster, service)
		if err != nil {
			t.Fatalf("CheckECS: unexpected error: %v", err)
		}
		// 不変条件: ACTIVE + running>=desired + desired>0 → Healthy==true
		if !result.Healthy {
			t.Errorf("CheckECS(status=ACTIVE, running=%d, desired=%d): Healthy=false（true であるべき）", running, desired)
		}
	})
}

// FuzzCheckECSUnhealthyCondition は running < desired のとき
// 常に Healthy==false を返すことを検証する。
func FuzzCheckECSUnhealthyCondition(f *testing.F) {
	f.Add("cluster", "svc", int32(2), int32(1))
	f.Add("cluster", "svc", int32(5), int32(3))
	f.Add("cluster", "svc", int32(1), int32(0))

	f.Fuzz(func(t *testing.T, cluster, service string, desired, running int32) {
		if !utf8.ValidString(cluster) || !utf8.ValidString(service) {
			t.Skip()
		}
		if cluster == "" || service == "" || desired <= 0 || running >= desired {
			t.Skip()
		}

		mock := &mockECSClient{
			output: &ecs.DescribeServicesOutput{
				Services: []ecstypes.Service{
					{
						Status:       aws.String("ACTIVE"),
						RunningCount: running,
						DesiredCount: desired,
					},
				},
			},
		}
		checker := &Checker{ecsCli: mock}
		result, err := checker.CheckECS(context.Background(), cluster, service)
		if err != nil {
			t.Fatalf("CheckECS: unexpected error: %v", err)
		}
		// 不変条件: running < desired → Healthy==false
		if result.Healthy {
			t.Errorf("CheckECS(running=%d < desired=%d): Healthy=true（false であるべき）", running, desired)
		}
	})
}

// ── FuzzCheckALB ─────────────────────────────────────────────────────────────

// FuzzCheckALBEmptyARNReturnsNil は targetGroupARN が空のとき
// 常に nil を返すことを検証する。
func FuzzCheckALBEmptyARNReturnsNil(f *testing.F) {
	f.Add("")

	f.Fuzz(func(t *testing.T, arn string) {
		if !utf8.ValidString(arn) || arn != "" {
			t.Skip()
		}
		checker := &Checker{elbv2: &mockELBV2Client{}}
		result, err := checker.CheckALB(context.Background(), arn)
		if err != nil {
			t.Errorf("CheckALB(%q): 空 ARN でエラーが返った: %v", arn, err)
		}
		if result != nil {
			t.Errorf("CheckALB(%q): 空 ARN で nil 以外が返った: %+v", arn, result)
		}
	})
}

// FuzzCheckALBAllHealthyConsistency は全ターゲットが healthy のとき
// AllHealthy==true を返すことを検証する。
// 不変条件: 1台でも unhealthy があれば AllHealthy==false。
func FuzzCheckALBAllHealthyConsistency(f *testing.F) {
	f.Add("arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/my-tg/abc123", true)
	f.Add("arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/my-tg/abc123", false)

	f.Fuzz(func(t *testing.T, arn string, allHealthy bool) {
		if !utf8.ValidString(arn) || arn == "" {
			t.Skip()
		}

		var state elbv2types.TargetHealthStateEnum
		if allHealthy {
			state = elbv2types.TargetHealthStateEnumHealthy
		} else {
			state = elbv2types.TargetHealthStateEnumUnhealthy
		}
		mock := &mockELBV2Client{
			output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{TargetHealth: &elbv2types.TargetHealth{State: state}},
				},
			},
		}
		checker := &Checker{elbv2: mock}
		result, err := checker.CheckALB(context.Background(), arn)
		if err != nil {
			t.Fatalf("CheckALB: unexpected error: %v", err)
		}
		// 不変条件: モックの allHealthy とレスポンスの AllHealthy が一致する
		if result.AllHealthy != allHealthy {
			t.Errorf("CheckALB: AllHealthy=%v, want %v", result.AllHealthy, allHealthy)
		}
	})
}

// ── FuzzCheckDynamoDB ─────────────────────────────────────────────────────────

// FuzzCheckDynamoDBEmptyNameReturnsNil は tableName が空のとき
// 常に nil を返すことを検証する。
func FuzzCheckDynamoDBEmptyNameReturnsNil(f *testing.F) {
	f.Add("")

	f.Fuzz(func(t *testing.T, tableName string) {
		if !utf8.ValidString(tableName) || tableName != "" {
			t.Skip()
		}
		checker := &Checker{dynoCli: &mockDynamoDBClient{}}
		result, err := checker.CheckDynamoDB(context.Background(), tableName)
		if err != nil {
			t.Errorf("CheckDynamoDB(%q): 空テーブル名でエラーが返った: %v", tableName, err)
		}
		if result != nil {
			t.Errorf("CheckDynamoDB(%q): 空テーブル名で nil 以外が返った: %+v", tableName, result)
		}
	})
}

// FuzzCheckDynamoDBActiveIsHealthy は ACTIVE ステータスのとき
// 常に Healthy==true を返すことを検証する。
// 不変条件: ACTIVE 以外（CREATING/DELETING 等）は Healthy==false。
func FuzzCheckDynamoDBActiveIsHealthy(f *testing.F) {
	f.Add("my-table", true)  // ACTIVE → Healthy
	f.Add("my-table", false) // 非ACTIVE → not Healthy

	f.Fuzz(func(t *testing.T, tableName string, isActive bool) {
		if !utf8.ValidString(tableName) || tableName == "" {
			t.Skip()
		}

		var status dynamodbtypes.TableStatus
		if isActive {
			status = dynamodbtypes.TableStatusActive
		} else {
			status = dynamodbtypes.TableStatusCreating
		}
		mock := &mockDynamoDBClient{
			output: &dynamodb.DescribeTableOutput{
				Table: &dynamodbtypes.TableDescription{
					TableStatus: status,
				},
			},
		}
		checker := &Checker{dynoCli: mock}
		result, err := checker.CheckDynamoDB(context.Background(), tableName)
		if err != nil {
			t.Fatalf("CheckDynamoDB: unexpected error: %v", err)
		}
		// 不変条件: ACTIVE → Healthy==true / 非ACTIVE → Healthy==false
		if result.Healthy != isActive {
			t.Errorf("CheckDynamoDB(status=%v): Healthy=%v, want %v", status, result.Healthy, isActive)
		}
	})
}
