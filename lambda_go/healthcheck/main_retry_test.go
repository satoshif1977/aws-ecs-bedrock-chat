package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/aws/smithy-go"
)

// このファイルは「retry.go を置いただけ」になっていないこと、
// すなわち Checker の 3 つの AWS 呼び出しが実際にリトライ層を通っていることを検証する。

// ── 指定回数だけ失敗するモック ────────────────────────────────

type flakyECSClient struct {
	failures int // 何回失敗させるか
	calls    int
	err      error
	output   *ecs.DescribeServicesOutput
}

func (m *flakyECSClient) DescribeServices(
	_ context.Context, _ *ecs.DescribeServicesInput, _ ...func(*ecs.Options),
) (*ecs.DescribeServicesOutput, error) {
	m.calls++
	if m.calls <= m.failures {
		return nil, m.err
	}
	return m.output, nil
}

type flakyELBV2Client struct {
	failures int
	calls    int
	err      error
	output   *elasticloadbalancingv2.DescribeTargetHealthOutput
}

func (m *flakyELBV2Client) DescribeTargetHealth(
	_ context.Context, _ *elasticloadbalancingv2.DescribeTargetHealthInput,
	_ ...func(*elasticloadbalancingv2.Options),
) (*elasticloadbalancingv2.DescribeTargetHealthOutput, error) {
	m.calls++
	if m.calls <= m.failures {
		return nil, m.err
	}
	return m.output, nil
}

type flakyDynamoDBClient struct {
	failures int
	calls    int
	err      error
	output   *dynamodb.DescribeTableOutput
}

func (m *flakyDynamoDBClient) DescribeTable(
	_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options),
) (*dynamodb.DescribeTableOutput, error) {
	m.calls++
	if m.calls <= m.failures {
		return nil, m.err
	}
	return m.output, nil
}

// fastRetrier は実待機ゼロの Retrier（テスト全体の所要時間を増やさない）。
func fastRetrier() Retrier {
	return Retrier{
		Config: RetryConfig{
			MaxAttempts: 4,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Millisecond,
		},
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
}

var throttled = apiErr{code: "ThrottlingException"}

// ── 各チェックがリトライ層を通っていること ────────────────────

func TestCheckECSはスロットリングでリトライする(t *testing.T) {
	cli := &flakyECSClient{
		failures: 2,
		err:      throttled,
		output: &ecs.DescribeServicesOutput{
			Services: []ecstypes.Service{{
				Status: aws.String("ACTIVE"), RunningCount: 2, DesiredCount: 2,
			}},
		},
	}
	c := &Checker{ecsCli: cli, retrier: fastRetrier()}

	got, err := c.CheckECS(context.Background(), "cluster", "service")
	if err != nil {
		t.Fatalf("リトライで成功するはずだが %v", err)
	}
	if cli.calls != 3 {
		t.Fatalf("3 回呼ばれるはず（2 回失敗 + 成功）だが %d 回", cli.calls)
	}
	if got == nil || !got.Healthy {
		t.Fatalf("healthy を期待したが %+v", got)
	}
}

func TestCheckALBはスロットリングでリトライする(t *testing.T) {
	cli := &flakyELBV2Client{
		failures: 1,
		err:      throttled,
		output: &elasticloadbalancingv2.DescribeTargetHealthOutput{
			TargetHealthDescriptions: []elbv2types.TargetHealthDescription{{
				Target:       &elbv2types.TargetDescription{Id: aws.String("i-1"), Port: aws.Int32(80)},
				TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy},
			}},
		},
	}
	c := &Checker{elbv2: cli, retrier: fastRetrier()}

	got, err := c.CheckALB(context.Background(), "arn:aws:elasticloadbalancing:tg")
	if err != nil {
		t.Fatalf("リトライで成功するはずだが %v", err)
	}
	if cli.calls != 2 {
		t.Fatalf("2 回呼ばれるはずだが %d 回", cli.calls)
	}
	if got == nil || !got.AllHealthy {
		t.Fatalf("all healthy を期待したが %+v", got)
	}
}

func TestCheckDynamoDBはスロットリングでリトライする(t *testing.T) {
	cli := &flakyDynamoDBClient{
		failures: 3,
		err:      apiErr{code: "ProvisionedThroughputExceededException"},
		output: &dynamodb.DescribeTableOutput{
			Table: &dynamodbtypes.TableDescription{TableStatus: dynamodbtypes.TableStatusActive},
		},
	}
	c := &Checker{dynoCli: cli, retrier: fastRetrier()}

	got, err := c.CheckDynamoDB(context.Background(), "table")
	if err != nil {
		t.Fatalf("リトライで成功するはずだが %v", err)
	}
	if cli.calls != 4 {
		t.Fatalf("4 回呼ばれるはずだが %d 回", cli.calls)
	}
	if got == nil || !got.Healthy {
		t.Fatalf("healthy を期待したが %+v", got)
	}
}

// ── リトライしてはいけないケース ──────────────────────────────

func TestCheckECSはリトライ不能なエラーなら1回で返す(t *testing.T) {
	cli := &flakyECSClient{
		failures: 99,
		err:      apiErr{code: "AccessDeniedException", fault: smithy.FaultClient},
	}
	c := &Checker{ecsCli: cli, retrier: fastRetrier()}

	_, err := c.CheckECS(context.Background(), "cluster", "service")
	if err == nil {
		t.Fatal("エラーを期待した")
	}
	if cli.calls != 1 {
		t.Fatalf("1 回で打ち切るはずだが %d 回", cli.calls)
	}
	// ラップしても元のエラー型は辿れること
	var target smithy.APIError
	if !errors.As(err, &target) || target.ErrorCode() != "AccessDeniedException" {
		t.Fatalf("元のエラー型を保っていない: %v", err)
	}
}

func TestCheckDynamoDBは上限まで試して元のエラーを返す(t *testing.T) {
	cli := &flakyDynamoDBClient{failures: 99, err: throttled}
	c := &Checker{dynoCli: cli, retrier: fastRetrier()}

	_, err := c.CheckDynamoDB(context.Background(), "table")
	if err == nil {
		t.Fatal("エラーを期待した")
	}
	if cli.calls != 4 {
		t.Fatalf("MaxAttempts(4) 回試すはずだが %d 回", cli.calls)
	}
}

// ── retryFor の既定値補正 ─────────────────────────────────────

func TestRetryForは未設定のRetrierを既定値へ補正する(t *testing.T) {
	// &Checker{ecsCli: ...} のように retrier を省いて生成しても
	// 「設定不正」で即エラーにならないこと（既存テストの生成方法を壊さない）。
	c := &Checker{}
	r := c.retryFor("DescribeServices")
	if err := r.Config.Validate(); err != nil {
		t.Fatalf("既定値へ補正されるはずだが %v", err)
	}
	if r.Config.MaxAttempts != DefaultRetryConfig().MaxAttempts {
		t.Fatalf("既定の MaxAttempts と一致しない: %d", r.Config.MaxAttempts)
	}
}

func TestRetryForは設定済みのRetrierをそのまま使う(t *testing.T) {
	c := &Checker{retrier: Retrier{Config: RetryConfig{
		MaxAttempts: 7, BaseDelay: time.Second, MaxDelay: time.Minute,
	}}}
	if got := c.retryFor("op").Config.MaxAttempts; got != 7 {
		t.Fatalf("設定が上書きされている: %d", got)
	}
}

func TestRetryForは操作名ごとにフックを作る(t *testing.T) {
	var ops []string
	c := &Checker{
		newRetryHook: func(op string) RetryHook {
			ops = append(ops, op)
			return func(int, time.Duration, error) {}
		},
	}
	for _, op := range []string{"DescribeServices", "DescribeTargetHealth", "DescribeTable"} {
		if r := c.retryFor(op); r.OnRetry == nil {
			t.Fatalf("%s のフックが設定されていない", op)
		}
	}
	want := []string{"DescribeServices", "DescribeTargetHealth", "DescribeTable"}
	if strings.Join(ops, ",") != strings.Join(want, ",") {
		t.Fatalf("操作名が渡っていない: %v", ops)
	}
}

// ── 実際の呼び出しでフックに正しい操作名が渡ること ────────────

func TestCheckALBのリトライフックに操作名が渡る(t *testing.T) {
	var gotOps []string
	cli := &flakyELBV2Client{
		failures: 1, err: throttled,
		output: &elasticloadbalancingv2.DescribeTargetHealthOutput{},
	}
	c := &Checker{
		elbv2:   cli,
		retrier: fastRetrier(),
		newRetryHook: func(op string) RetryHook {
			return func(int, time.Duration, error) { gotOps = append(gotOps, op) }
		},
	}
	if _, err := c.CheckALB(context.Background(), "arn:tg"); err != nil {
		t.Fatalf("成功するはずだが %v", err)
	}
	if len(gotOps) != 1 || gotOps[0] != "DescribeTargetHealth" {
		t.Fatalf("操作名が正しく渡っていない: %v", gotOps)
	}
}

// ── newRetryHookFactory（ログ + メトリクスの同時結線）─────────

func TestNewRetryHookFactoryはログとメトリクスの両方へ記録する(t *testing.T) {
	var logBuf bytes.Buffer
	logger := NewLogger(LoggerOptions{Writer: &logBuf, Level: LogLevelDebug})

	var metricBuf bytes.Buffer
	metrics, err := NewMetrics(MetricsOptions{Namespace: "Test/Retry", Sink: &metricBuf})
	if err != nil {
		t.Fatalf("メトリクスの初期化に失敗: %v", err)
	}

	hook := newRetryHookFactory(logger, metrics)("DescribeTable")
	if hook == nil {
		t.Fatal("フックが生成されていない")
	}
	hook(2, 250*time.Millisecond, throttled)

	// ── ログ側 ──
	logLine := logBuf.String()
	if !strings.Contains(logLine, "DescribeTable") {
		t.Fatalf("ログに操作名がない: %s", logLine)
	}
	if !strings.Contains(logLine, "250") {
		t.Fatalf("ログに遅延(ms)がない: %s", logLine)
	}

	// ── メトリクス側 ──
	if err := metrics.Flush(); err != nil {
		t.Fatalf("Flush に失敗: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(metricBuf.Bytes(), &doc); err != nil {
		t.Fatalf("EMF が JSON として読めない: %v / %s", err, metricBuf.String())
	}
	if doc["retryOperation"] != "DescribeTable" {
		t.Fatalf("retryOperation プロパティがない: %v", doc)
	}
	for _, key := range []string{"RetryAttempt", "RetryDelay", "RetryAttemptNumber"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("メトリクス %s が記録されていない: %v", key, doc)
		}
	}
}

func TestNewRetryHookFactoryはメトリクスがnilでもログだけで動く(t *testing.T) {
	var logBuf bytes.Buffer
	logger := NewLogger(LoggerOptions{Writer: &logBuf, Level: LogLevelDebug})

	hook := newRetryHookFactory(logger, nil)("DescribeServices")
	if hook == nil {
		t.Fatal("メトリクスが nil でもログ用フックは返るはず")
	}
	hook(1, time.Second, throttled) // パニックしないこと

	if !strings.Contains(logBuf.String(), "DescribeServices") {
		t.Fatalf("ログに出力されていない: %s", logBuf.String())
	}
}

// ── runHealthCheck 全体でリトライが効くこと ──────────────────

func TestRunHealthCheckはリトライ後に正常判定になる(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	ecsCli := &flakyECSClient{
		failures: 1, err: throttled,
		output: &ecs.DescribeServicesOutput{Services: []ecstypes.Service{{
			Status: aws.String("ACTIVE"), RunningCount: 1, DesiredCount: 1,
		}}},
	}
	elbCli := &flakyELBV2Client{
		failures: 1, err: throttled,
		output: &elasticloadbalancingv2.DescribeTargetHealthOutput{},
	}
	dynoCli := &flakyDynamoDBClient{
		failures: 1, err: throttled,
		output: &dynamodb.DescribeTableOutput{
			Table: &dynamodbtypes.TableDescription{TableStatus: dynamodbtypes.TableStatusActive},
		},
	}

	checker := &Checker{ecsCli: ecsCli, elbv2: elbCli, dynoCli: dynoCli, retrier: fastRetrier()}
	summary := runHealthCheck(context.Background(), checker, logger, nil, checkConfig{
		ClusterName: "c", ServiceName: "s", TargetGroupARN: "arn", TableName: "t",
	})

	if !summary.Healthy {
		t.Fatalf("リトライで回復するので healthy のはず: %+v", summary)
	}
	if ecsCli.calls != 2 || elbCli.calls != 2 || dynoCli.calls != 2 {
		t.Fatalf("各 2 回呼ばれるはず ecs=%d elb=%d dyno=%d",
			ecsCli.calls, elbCli.calls, dynoCli.calls)
	}
}
