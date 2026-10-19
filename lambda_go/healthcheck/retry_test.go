package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// ── テスト用ヘルパー ──────────────────────────────────────────

// apiErr は smithy.APIError を満たすテスト用エラー。
type apiErr struct {
	code  string
	fault smithy.ErrorFault
}

func (e apiErr) Error() string                 { return "api error: " + e.code }
func (e apiErr) ErrorCode() string             { return e.code }
func (e apiErr) ErrorMessage() string          { return e.code }
func (e apiErr) ErrorFault() smithy.ErrorFault { return e.fault }

// timeoutErr は net.Error を満たすタイムアウトエラー。
type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }
func (timeoutErr) Temporary() bool {
	return true
}

var _ net.Error = timeoutErr{}

// noSleep は実待機ゼロの SleepFunc。呼ばれた回数と待機時間を記録する。
type noSleep struct {
	calls  int
	delays []time.Duration
}

func (s *noSleep) fn(ctx context.Context, d time.Duration) error {
	s.calls++
	s.delays = append(s.delays, d)
	return ctx.Err()
}

// testRetrier は実待機ゼロ・ジッター固定の Retrier を返す。
func testRetrier(sleep *noSleep, maxAttempts int) Retrier {
	return Retrier{
		Config: RetryConfig{
			MaxAttempts: maxAttempts,
			BaseDelay:   10 * time.Millisecond,
			MaxDelay:    100 * time.Millisecond,
			Jitter:      false,
		},
		Sleep: sleep.fn,
		Rand:  func() float64 { return 0.5 },
	}
}

// ── RetryConfig ───────────────────────────────────────────────

func TestDefaultRetryConfigは妥当(t *testing.T) {
	if err := DefaultRetryConfig().Validate(); err != nil {
		t.Fatalf("既定値が不正: %v", err)
	}
}

func TestRetryConfigValidate(t *testing.T) {
	base := DefaultRetryConfig()

	tests := []struct {
		name string
		cfg  RetryConfig
		want error
	}{
		{"MaxAttemptsが0", RetryConfig{MaxAttempts: 0, BaseDelay: base.BaseDelay, MaxDelay: base.MaxDelay}, ErrInvalidMaxAttempts},
		{"BaseDelayが0", RetryConfig{MaxAttempts: 3, BaseDelay: 0, MaxDelay: base.MaxDelay}, ErrInvalidBaseDelay},
		{"MaxDelayがBaseDelay未満", RetryConfig{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Millisecond}, ErrInvalidMaxDelay},
		{"正常", base, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("エラーなしを期待したが %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("want %v, got %v", tt.want, err)
			}
		})
	}
}

// ── IsRetryable ───────────────────────────────────────────────

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ただのエラー", errors.New("boom"), false},
		{"ThrottlingException", apiErr{code: "ThrottlingException"}, true},
		{"RequestLimitExceeded", apiErr{code: "RequestLimitExceeded"}, true},
		{"ProvisionedThroughputExceeded", apiErr{code: "ProvisionedThroughputExceededException"}, true},
		{"ValidationException", apiErr{code: "ValidationException", fault: smithy.FaultClient}, false},
		{"AccessDenied", apiErr{code: "AccessDeniedException", fault: smithy.FaultClient}, false},
		{"サーバ起因なら対象", apiErr{code: "SomethingWeird", fault: smithy.FaultServer}, true},
		{"contextキャンセル", context.Canceled, false},
		{"context期限切れ", context.DeadlineExceeded, false},
		{"ネットワークタイムアウト", timeoutErr{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Fatalf("want %v, got %v", tt.want, got)
			}
		})
	}
}

// httpRespErr は指定ステータスの AWS HTTP レスポンスエラーを組み立てる。
func httpRespErr(status int) error {
	return &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{
				Response: &http.Response{StatusCode: status},
			},
			Err: errors.New("http error"),
		},
	}
}

func TestIsRetryableはHTTPステータスで判定する(t *testing.T) {
	for _, code := range []int{429, 500, 502, 503, 504} {
		if !IsRetryable(httpRespErr(code)) {
			t.Fatalf("status=%d はリトライ対象のはず", code)
		}
	}
	for _, code := range []int{400, 403, 404} {
		if IsRetryable(httpRespErr(code)) {
			t.Fatalf("status=%d はリトライ対象外のはず", code)
		}
	}
}

func TestIsRetryableはラップされていても判定できる(t *testing.T) {
	wrapped := fmt.Errorf("DescribeTable: %w", apiErr{code: "ThrottlingException"})
	if !IsRetryable(wrapped) {
		t.Fatal("ラップされた ThrottlingException を検出できていない")
	}
}

// ── ComputeDelay ──────────────────────────────────────────────

func TestComputeDelayは指数で増えMaxDelayで頭打ちになる(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 10, BaseDelay: 100 * time.Millisecond, MaxDelay: 400 * time.Millisecond}
	rnd := func() float64 { return 1.0 }

	want := []time.Duration{
		100 * time.Millisecond, // 2^0
		200 * time.Millisecond, // 2^1
		400 * time.Millisecond, // 2^2
		400 * time.Millisecond, // 頭打ち
		400 * time.Millisecond,
	}
	for i, w := range want {
		if got := ComputeDelay(i+1, cfg, rnd); got != w {
			t.Fatalf("attempt=%d: want %v, got %v", i+1, w, got)
		}
	}
}

func TestComputeDelayはattemptが1未満でも1として扱う(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second}
	rnd := func() float64 { return 1.0 }
	if got := ComputeDelay(0, cfg, rnd); got != 100*time.Millisecond {
		t.Fatalf("want 100ms, got %v", got)
	}
	if got := ComputeDelay(-5, cfg, rnd); got != 100*time.Millisecond {
		t.Fatalf("want 100ms, got %v", got)
	}
}

func TestComputeDelayはジッターで0からその値の範囲に散る(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second, Jitter: true}
	if got := ComputeDelay(1, cfg, func() float64 { return 0 }); got != 0 {
		t.Fatalf("rnd=0 なら 0 のはずだが %v", got)
	}
	if got := ComputeDelay(1, cfg, func() float64 { return 1 }); got != 100*time.Millisecond {
		t.Fatalf("rnd=1 なら 100ms のはずだが %v", got)
	}
}

func TestComputeDelayは巨大なattemptでもオーバーフローしない(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 1000, BaseDelay: time.Millisecond, MaxDelay: time.Second}
	got := ComputeDelay(10000, cfg, func() float64 { return 1.0 })
	if got != time.Second {
		t.Fatalf("MaxDelay で頭打ちになるはずだが %v", got)
	}
}

func TestComputeDelayはrndがnilでも動く(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: time.Second, Jitter: true}
	got := ComputeDelay(1, cfg, nil)
	if got < 0 || got > 10*time.Millisecond {
		t.Fatalf("0〜10ms の範囲に収まるはずだが %v", got)
	}
}

// ── ContextSleep ──────────────────────────────────────────────

func TestContextSleepは時間が来たらnilを返す(t *testing.T) {
	if err := ContextSleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("nil を期待したが %v", err)
	}
}

func TestContextSleepはキャンセルを尊重する(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ContextSleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("context.Canceled を期待したが %v", err)
	}
}

// ── CombineRetryHooks ─────────────────────────────────────────

func TestCombineRetryHooks(t *testing.T) {
	t.Run("すべてnilならnilを返す", func(t *testing.T) {
		if CombineRetryHooks() != nil {
			t.Fatal("0 個なら nil のはず")
		}
		if CombineRetryHooks(nil, nil) != nil {
			t.Fatal("nil だけなら nil のはず")
		}
	})

	t.Run("複数のフックがすべて呼ばれる", func(t *testing.T) {
		var a, b int
		h := CombineRetryHooks(
			func(int, time.Duration, error) { a++ },
			nil,
			func(int, time.Duration, error) { b++ },
		)
		h(1, time.Second, errors.New("x"))
		if a != 1 || b != 1 {
			t.Fatalf("両方呼ばれるはず a=%d b=%d", a, b)
		}
	})

	t.Run("引数がそのまま渡る", func(t *testing.T) {
		var gotAttempt int
		var gotDelay time.Duration
		var gotErr error
		want := errors.New("throttled")
		CombineRetryHooks(func(a int, d time.Duration, e error) {
			gotAttempt, gotDelay, gotErr = a, d, e
		})(3, 250*time.Millisecond, want)

		if gotAttempt != 3 || gotDelay != 250*time.Millisecond || !errors.Is(gotErr, want) {
			t.Fatalf("attempt=%d delay=%v err=%v", gotAttempt, gotDelay, gotErr)
		}
	})
}

// ── Retrier.Do ────────────────────────────────────────────────

func TestDoは成功したらリトライしない(t *testing.T) {
	sleep := &noSleep{}
	calls := 0
	err := testRetrier(sleep, 4).Do(context.Background(), "op", func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 || sleep.calls != 0 {
		t.Fatalf("err=%v calls=%d sleeps=%d", err, calls, sleep.calls)
	}
}

func TestDoはリトライ可能なエラーで再試行し最後は成功する(t *testing.T) {
	sleep := &noSleep{}
	calls := 0
	err := testRetrier(sleep, 4).Do(context.Background(), "op", func(context.Context) error {
		calls++
		if calls < 3 {
			return apiErr{code: "ThrottlingException"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("成功を期待したが %v", err)
	}
	if calls != 3 || sleep.calls != 2 {
		t.Fatalf("calls=%d sleeps=%d", calls, sleep.calls)
	}
}

func TestDoはリトライ不能なエラーなら即返す(t *testing.T) {
	sleep := &noSleep{}
	want := apiErr{code: "ValidationException", fault: smithy.FaultClient}
	calls := 0
	err := testRetrier(sleep, 4).Do(context.Background(), "op", func(context.Context) error {
		calls++
		return want
	})
	if calls != 1 || sleep.calls != 0 {
		t.Fatalf("1 回で打ち切るはず calls=%d sleeps=%d", calls, sleep.calls)
	}
	if !errors.Is(err, want) {
		t.Fatalf("元のエラーをそのまま返すはずだが %v", err)
	}
}

func TestDoは最終試行で失敗したら元のエラーを返す(t *testing.T) {
	sleep := &noSleep{}
	want := apiErr{code: "ThrottlingException"}
	calls := 0
	err := testRetrier(sleep, 3).Do(context.Background(), "op", func(context.Context) error {
		calls++
		return want
	})
	if calls != 3 {
		t.Fatalf("MaxAttempts 回試行するはず calls=%d", calls)
	}
	// errors.As が壊れていないこと（包んでいないこと）を確認する
	var target smithy.APIError
	if !errors.As(err, &target) || target.ErrorCode() != "ThrottlingException" {
		t.Fatalf("元のエラー型を保っていない: %v", err)
	}
}

func TestDoは設定が不正ならエラーを返す(t *testing.T) {
	r := Retrier{Config: RetryConfig{MaxAttempts: 0}}
	calls := 0
	err := r.Do(context.Background(), "op", func(context.Context) error {
		calls++
		return nil
	})
	if calls != 0 {
		t.Fatal("設定不正なら fn を呼ばないはず")
	}
	if !errors.Is(err, ErrInvalidMaxAttempts) {
		t.Fatalf("ErrInvalidMaxAttempts を期待したが %v", err)
	}
}

func TestDoはOnRetryフックを呼ぶ(t *testing.T) {
	sleep := &noSleep{}
	r := testRetrier(sleep, 3)
	var attempts []int
	r.OnRetry = func(attempt int, _ time.Duration, _ error) {
		attempts = append(attempts, attempt)
	}
	_ = r.Do(context.Background(), "op", func(context.Context) error {
		return apiErr{code: "ThrottlingException"}
	})
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 {
		t.Fatalf("リトライ直前に 1,2 で呼ばれるはず: %v", attempts)
	}
}

func TestDoは待機中のキャンセルで中断する(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	original := apiErr{code: "ThrottlingException"}
	r := Retrier{
		Config: RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Second},
		Sleep:  ContextSleep,
	}
	err := r.Do(ctx, "op", func(context.Context) error { return original })
	if !errors.Is(err, original) {
		t.Fatalf("元のエラーを保っていない: %v", err)
	}
}

func TestDoはSleepが未設定でも動く(t *testing.T) {
	r := Retrier{Config: RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}}
	calls := 0
	err := r.Do(context.Background(), "op", func(context.Context) error {
		calls++
		if calls == 1 {
			return apiErr{code: "ThrottlingException"}
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

// ── RetryValue ────────────────────────────────────────────────

func TestRetryValueは値を返す(t *testing.T) {
	sleep := &noSleep{}
	calls := 0
	got, err := RetryValue(context.Background(), testRetrier(sleep, 4), "op",
		func(context.Context) (string, error) {
			calls++
			if calls < 2 {
				return "", apiErr{code: "ThrottlingException"}
			}
			return "ok", nil
		})
	if err != nil || got != "ok" || calls != 2 {
		t.Fatalf("got=%q err=%v calls=%d", got, err, calls)
	}
}

func TestRetryValueは失敗時にゼロ値とエラーを返す(t *testing.T) {
	sleep := &noSleep{}
	got, err := RetryValue(context.Background(), testRetrier(sleep, 2), "op",
		func(context.Context) (*ecsLikeOutput, error) {
			return nil, apiErr{code: "ThrottlingException"}
		})
	if got != nil {
		t.Fatalf("ゼロ値を期待したが %v", got)
	}
	if err == nil {
		t.Fatal("エラーを期待した")
	}
}

// ecsLikeOutput は RetryValue のジェネリクス確認用のダミー型。
type ecsLikeOutput struct{ Name string }

func TestNewRetrierは既定設定を持つ(t *testing.T) {
	r := NewRetrier()
	if err := r.Config.Validate(); err != nil {
		t.Fatalf("既定設定が不正: %v", err)
	}
	if r.OnRetry != nil || r.Sleep != nil || r.Rand != nil {
		t.Fatal("フック類は未設定であるべき（既定実装へフォールバックする）")
	}
}
