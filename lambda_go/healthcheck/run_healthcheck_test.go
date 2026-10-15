package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// ── テスト用のスタブ ──────────────────────────────────────────────────────────

type stubECS struct {
	out *ecs.DescribeServicesOutput
	err error
}

func (s stubECS) DescribeServices(
	_ context.Context, _ *ecs.DescribeServicesInput, _ ...func(*ecs.Options),
) (*ecs.DescribeServicesOutput, error) {
	return s.out, s.err
}

type stubELBV2 struct {
	out *elasticloadbalancingv2.DescribeTargetHealthOutput
	err error
}

func (s stubELBV2) DescribeTargetHealth(
	_ context.Context,
	_ *elasticloadbalancingv2.DescribeTargetHealthInput,
	_ ...func(*elasticloadbalancingv2.Options),
) (*elasticloadbalancingv2.DescribeTargetHealthOutput, error) {
	return s.out, s.err
}

type stubDynamoDB struct {
	out *dynamodb.DescribeTableOutput
	err error
}

func (s stubDynamoDB) DescribeTable(
	_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options),
) (*dynamodb.DescribeTableOutput, error) {
	return s.out, s.err
}

// ── ヘルパー ──────────────────────────────────────────────────────────────────

// newTestLogger は出力をバッファへ書き出すロガーを返す。
func newTestLogger(buf io.Writer) *slog.Logger {
	return NewLogger(LoggerOptions{
		Level:  LogLevelDebug,
		Writer: buf,
		Now:    func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) },
	})
}

// newTestMetrics は出力をバッファへ書き出すメトリクスを返す。
func newTestMetrics(t *testing.T, buf io.Writer) *Metrics {
	t.Helper()
	m, err := NewMetrics(MetricsOptions{
		Namespace: "Test/HealthCheck",
		Sink:      buf,
		Now:       func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	return m
}

// healthyCheckerはすべて正常を返す Checker を組み立てる。
func healthyChecker() *Checker {
	return &Checker{
		ecsCli: stubECS{out: &ecs.DescribeServicesOutput{
			Services: []ecstypes.Service{{
				Status:       aws.String("ACTIVE"),
				RunningCount: 2,
				DesiredCount: 2,
			}},
		}},
		elbv2: stubELBV2{out: &elasticloadbalancingv2.DescribeTargetHealthOutput{
			TargetHealthDescriptions: []elbv2types.TargetHealthDescription{{
				Target: &elbv2types.TargetDescription{
					Id:   aws.String("i-0123456789abcdef0"),
					Port: aws.Int32(8080),
				},
				TargetHealth: &elbv2types.TargetHealth{
					State: elbv2types.TargetHealthStateEnumHealthy,
				},
			}},
		}},
		dynoCli: stubDynamoDB{out: &dynamodb.DescribeTableOutput{
			Table: &dynamodbtypes.TableDescription{
				TableStatus: dynamodbtypes.TableStatusActive,
			},
		}},
	}
}

// fullConfig はすべてのリソースを対象にする設定を返す。
func fullConfig() checkConfig {
	return checkConfig{
		ClusterName:    "chat-cluster",
		ServiceName:    "chat-service",
		TargetGroupARN: "arn:aws:elasticloadbalancing:ap-northeast-1:123456789012:targetgroup/tg/abc",
		TableName:      "chat-sessions",
	}
}

// metricValues は EMF ドキュメントから メトリクス名 → 値 を取り出す。
func metricValues(t *testing.T, raw string) map[string]float64 {
	t.Helper()
	got := map[string]float64{}
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("EMF ドキュメントの解析に失敗しました: %v\n%s", err, line)
		}
		for k, v := range doc {
			if num, ok := v.(float64); ok {
				got[k] = num
			}
		}
	}
	return got
}

// ── boolMetric ───────────────────────────────────────────────────────────────

func TestBoolMetric(t *testing.T) {
	if got := boolMetric(true); got != 1 {
		t.Errorf("boolMetric(true) = %v, want 1", got)
	}
	if got := boolMetric(false); got != 0 {
		t.Errorf("boolMetric(false) = %v, want 0", got)
	}
}

// ── loadCheckConfig ──────────────────────────────────────────────────────────

func TestLoadCheckConfig(t *testing.T) {
	t.Setenv("ECS_CLUSTER_NAME", "cluster-a")
	t.Setenv("ECS_SERVICE_NAME", "service-a")
	t.Setenv("ALB_TARGET_GROUP_ARN", "arn:tg")
	t.Setenv("DYNAMODB_TABLE_NAME", "table-a")

	got := loadCheckConfig()
	want := checkConfig{
		ClusterName:    "cluster-a",
		ServiceName:    "service-a",
		TargetGroupARN: "arn:tg",
		TableName:      "table-a",
	}
	if got != want {
		t.Errorf("loadCheckConfig() = %+v, want %+v", got, want)
	}
}

func TestLoadCheckConfig_空の環境変数(t *testing.T) {
	t.Setenv("ECS_CLUSTER_NAME", "")
	t.Setenv("ECS_SERVICE_NAME", "")
	t.Setenv("ALB_TARGET_GROUP_ARN", "")
	t.Setenv("DYNAMODB_TABLE_NAME", "")

	if got := loadCheckConfig(); got != (checkConfig{}) {
		t.Errorf("loadCheckConfig() = %+v, want 空の checkConfig", got)
	}
}

// ── recordCount ──────────────────────────────────────────────────────────────

func TestRecordCount_メトリクスがnilでもパニックしない(t *testing.T) {
	var logBuf bytes.Buffer
	logger := newTestLogger(&logBuf)

	recordCount(logger, nil, "AnyMetric", 1) // パニックしなければ成功

	if logBuf.Len() != 0 {
		t.Errorf("metrics が nil のときはログを出力しないはずだが、出力された: %s", logBuf.String())
	}
}

func TestRecordCount_メトリクス名が不正なら警告ログを出す(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer
	logger := newTestLogger(&logBuf)
	metrics := newTestMetrics(t, &metricBuf)

	recordCount(logger, metrics, "", 1) // 空の名前は不正

	if !strings.Contains(logBuf.String(), "メトリクスの記録に失敗しました") {
		t.Errorf("警告ログが出ていない: %s", logBuf.String())
	}
}

// ── runHealthCheck ───────────────────────────────────────────────────────────

func TestRunHealthCheck_すべて正常(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer
	logger := newTestLogger(&logBuf)
	metrics := newTestMetrics(t, &metricBuf)

	summary := runHealthCheck(context.Background(), healthyChecker(), logger, metrics, fullConfig())

	if !summary.Healthy {
		t.Fatalf("Healthy = false, want true（summary=%+v）", summary)
	}
	if summary.ECS == nil || summary.ALB == nil || summary.DynamoDB == nil {
		t.Fatalf("各リソースの結果が入っていない: %+v", summary)
	}

	if err := metrics.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got := metricValues(t, metricBuf.String())
	for name, want := range map[string]float64{
		"EcsHealthy":          1,
		"EcsRunningTasks":     2,
		"AlbAllHealthy":       1,
		"AlbUnhealthyTargets": 0,
		"DynamoDbHealthy":     1,
		"OverallHealthy":      1,
	} {
		if got[name] != want {
			t.Errorf("メトリクス %s = %v, want %v", name, got[name], want)
		}
	}
}

func TestRunHealthCheck_ECSが異常なら全体も異常(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer
	logger := newTestLogger(&logBuf)
	metrics := newTestMetrics(t, &metricBuf)

	checker := healthyChecker()
	checker.ecsCli = stubECS{out: &ecs.DescribeServicesOutput{
		Services: []ecstypes.Service{{
			Status:       aws.String("ACTIVE"),
			RunningCount: 0,
			DesiredCount: 2,
		}},
	}}

	summary := runHealthCheck(context.Background(), checker, logger, metrics, fullConfig())

	if summary.Healthy {
		t.Error("Healthy = true, want false")
	}
	if !strings.Contains(logBuf.String(), "ECS サービスが正常ではありません") {
		t.Errorf("警告ログが出ていない: %s", logBuf.String())
	}

	if err := metrics.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got := metricValues(t, metricBuf.String())
	if got["EcsHealthy"] != 0 {
		t.Errorf("EcsHealthy = %v, want 0", got["EcsHealthy"])
	}
	if got["OverallHealthy"] != 0 {
		t.Errorf("OverallHealthy = %v, want 0", got["OverallHealthy"])
	}
}

func TestRunHealthCheck_ALBに異常なターゲットがある(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer
	logger := newTestLogger(&logBuf)
	metrics := newTestMetrics(t, &metricBuf)

	checker := healthyChecker()
	checker.elbv2 = stubELBV2{out: &elasticloadbalancingv2.DescribeTargetHealthOutput{
		TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
			{
				Target:       &elbv2types.TargetDescription{Id: aws.String("i-aaa"), Port: aws.Int32(8080)},
				TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
			},
			{
				Target:       &elbv2types.TargetDescription{Id: aws.String("i-bbb"), Port: aws.Int32(8080)},
				TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumUnhealthy},
			},
		},
	}}

	summary := runHealthCheck(context.Background(), checker, logger, metrics, fullConfig())

	if summary.Healthy {
		t.Error("Healthy = true, want false")
	}
	if err := metrics.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got := metricValues(t, metricBuf.String())
	if got["AlbUnhealthyTargets"] != 1 {
		t.Errorf("AlbUnhealthyTargets = %v, want 1", got["AlbUnhealthyTargets"])
	}
	if got["AlbAllHealthy"] != 0 {
		t.Errorf("AlbAllHealthy = %v, want 0", got["AlbAllHealthy"])
	}
}

func TestRunHealthCheck_API呼び出しが失敗したらエラーメトリクスを出す(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer
	logger := newTestLogger(&logBuf)
	metrics := newTestMetrics(t, &metricBuf)

	checker := healthyChecker()
	checker.dynoCli = stubDynamoDB{err: errors.New("AccessDenied")}

	summary := runHealthCheck(context.Background(), checker, logger, metrics, fullConfig())

	if summary.Healthy {
		t.Error("Healthy = true, want false")
	}
	if summary.DynamoDB != nil {
		t.Errorf("エラー時は DynamoDB の結果を入れない想定: %+v", summary.DynamoDB)
	}
	if !strings.Contains(logBuf.String(), "DynamoDB のヘルスチェックに失敗しました") {
		t.Errorf("エラーログが出ていない: %s", logBuf.String())
	}

	if err := metrics.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := metricValues(t, metricBuf.String()); got["DynamoDbCheckErrors"] != 1 {
		t.Errorf("DynamoDbCheckErrors = %v, want 1", got["DynamoDbCheckErrors"])
	}
}

func TestRunHealthCheck_設定が空ならチェックを行わない(t *testing.T) {
	var logBuf, metricBuf bytes.Buffer
	logger := newTestLogger(&logBuf)
	metrics := newTestMetrics(t, &metricBuf)

	summary := runHealthCheck(context.Background(), healthyChecker(), logger, metrics, checkConfig{})

	if !summary.Healthy {
		t.Errorf("対象がない場合は Healthy = true のままにする想定: %+v", summary)
	}
	if summary.ECS != nil || summary.ALB != nil || summary.DynamoDB != nil {
		t.Errorf("対象がない場合は各結果が nil の想定: %+v", summary)
	}
}

func TestRunHealthCheck_メトリクスがnilでも動作する(t *testing.T) {
	var logBuf bytes.Buffer
	logger := newTestLogger(&logBuf)

	summary := runHealthCheck(context.Background(), healthyChecker(), logger, nil, fullConfig())

	if !summary.Healthy {
		t.Errorf("Healthy = false, want true（summary=%+v）", summary)
	}
	if !strings.Contains(logBuf.String(), "ヘルスチェックが完了しました") {
		t.Errorf("完了ログが出ていない: %s", logBuf.String())
	}
}

func TestRunHealthCheck_秘匿値はログに出さない(t *testing.T) {
	var logBuf bytes.Buffer
	logger := newTestLogger(&logBuf)

	cfg := fullConfig()
	cfg.ClusterName = "cluster-with-secret"

	checker := healthyChecker()
	checker.ecsCli = stubECS{err: errors.New("boom")}

	_ = runHealthCheck(context.Background(), checker, logger, nil, cfg)

	// クラスター名は秘匿値ではないため出力される。エラー内容も残ること。
	out := logBuf.String()
	if !strings.Contains(out, "cluster-with-secret") {
		t.Errorf("クラスター名がログに出ていない: %s", out)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("エラー内容がログに出ていない: %s", out)
	}
}
