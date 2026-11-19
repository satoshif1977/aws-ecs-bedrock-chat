/**
 * ecs-notifier のメトリクス結線テスト
 *
 * 「metrics.ts を置いただけ」に戻らないよう、ハンドラーの実際の出力で確認する。
 *   1. 通知の成否と所要時間が EMF のメトリクスとして出ること
 *   2. 高カーディナリティの値をディメンションに入れていないこと
 *   3. メトリクスが壊れても通知自体は落ちないこと
 *
 * ログ側の結線は index.validation.test.ts / index.comprehensive.test.ts で見る。
 */

import { createHandler, DEFAULT_METRICS_NAMESPACE, publishNotification } from "./index";
import type { EcsTaskDetail, EventBridgeEcsEvent, SnsPublisher } from "./index";
import { createMetrics, RESERVED_ROOT_KEY } from "./metrics";
import type { Metrics } from "./metrics";
import { createLogger } from "./logger";
import { PublishCommand } from "@aws-sdk/client-sns";

// ── テスト用ヘルパー ──────────────────────────────────────────

const TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123456789012:ecs-notify";
const FIXED_NOW = Date.UTC(2026, 10, 20, 9, 0, 0);

const makeDetail = (overrides: Partial<EcsTaskDetail> = {}): EcsTaskDetail => ({
  clusterArn: "arn:aws:ecs:ap-northeast-1:123456789012:cluster/myapp-dev-cluster",
  taskArn: "arn:aws:ecs:ap-northeast-1:123456789012:task/myapp-dev-cluster/abc123",
  lastStatus: "RUNNING",
  desiredStatus: "RUNNING",
  ...overrides,
});

const makeEvent = (overrides: Partial<EcsTaskDetail> = {}) => ({
  source: "aws.ecs",
  "detail-type": "ECS Task State Change",
  detail: makeDetail(overrides),
});

interface MockSnsOptions {
  /** send の中で呼ぶ副作用。時計を進めるのに使う */
  onSend?: () => void;
  /** 投げさせたいエラー */
  fail?: Error;
}

const makeMockSns = (
  options: MockSnsOptions = {},
): { client: SnsPublisher; calls: PublishCommand[] } => {
  const calls: PublishCommand[] = [];
  const client: SnsPublisher = {
    send: async (cmd: PublishCommand) => {
      calls.push(cmd);
      options.onSend?.();
      if (options.fail) throw options.fail;
      return { MessageId: "msg-0001", $metadata: {} };
    },
  };
  return { client, calls };
};

interface Harness {
  metrics: Metrics;
  lines: string[];
  docs(): Record<string, unknown>[];
  /** 最後に flush された EMF ドキュメント */
  last(): Record<string, unknown>;
  advance(ms: number): void;
}

function harness(): Harness {
  const lines: string[] = [];
  let now = FIXED_NOW;
  const metrics = createMetrics({
    namespace: "TestNs",
    dimensions: { Handler: "ecs-notifier" },
    sink: (line) => lines.push(line),
    now: () => now,
  });
  const docs = () => lines.map((l) => JSON.parse(l) as Record<string, unknown>);
  return {
    metrics,
    lines,
    docs,
    last: () => {
      const all = docs();
      return all[all.length - 1];
    },
    advance: (ms) => {
      now += ms;
    },
  };
}

function directiveOf(doc: Record<string, unknown>): Record<string, unknown> {
  const aws = doc[RESERVED_ROOT_KEY] as Record<string, unknown>;
  return (aws.CloudWatchMetrics as Record<string, unknown>[])[0];
}

function metricNames(doc: Record<string, unknown>): string[] {
  return (directiveOf(doc).Metrics as { Name: string }[]).map((d) => d.Name);
}

function dimensionKeys(doc: Record<string, unknown>): string[] {
  return (directiveOf(doc).Dimensions as string[][])[0];
}

/** EMF の行かどうか（console.log に混ざるログと区別する） */
function isMetricsLine(line: unknown): boolean {
  try {
    return RESERVED_ROOT_KEY in (JSON.parse(String(line)) as Record<string, unknown>);
  } catch {
    return false;
  }
}

beforeEach(() => {
  process.env.SNS_TOPIC_ARN = TOPIC_ARN;
  delete process.env.METRICS_NAMESPACE;
});

afterEach(() => {
  delete process.env.SNS_TOPIC_ARN;
  delete process.env.METRICS_NAMESPACE;
});

// ── 通知の成否 ────────────────────────────────────────────────

describe("通知の成否がメトリクスに出る", () => {
  it("成功すると NotificationSuccess が 1 件出る", async () => {
    const h = harness();
    const { client } = makeMockSns();

    const result = await createHandler(client, { metrics: h.metrics })(makeEvent());

    expect(result.status).toBe("published");
    expect(h.last().NotificationSuccess).toBe(1);
    expect(metricNames(h.last())).toContain("NotificationSuccess");
  });

  it("成功時は NotificationError を出さない", async () => {
    const h = harness();
    const { client } = makeMockSns();

    await createHandler(client, { metrics: h.metrics })(makeEvent());

    expect(metricNames(h.last())).not.toContain("NotificationError");
  });

  it("失敗すると NotificationError が出る", async () => {
    const h = harness();
    const boom = Object.assign(new Error("拒否されました"), {
      name: "InvalidParameterException",
    });
    const { client } = makeMockSns({ fail: boom });

    await expect(
      createHandler(client, { metrics: h.metrics })(makeEvent()),
    ).rejects.toThrow("拒否されました");

    expect(h.last().NotificationError).toBe(1);
    expect(metricNames(h.last())).not.toContain("NotificationSuccess");
  });

  it("失敗してもメトリクスは flush される（例外で握り潰されない）", async () => {
    const h = harness();
    const { client } = makeMockSns({
      fail: Object.assign(new Error("ng"), { name: "InvalidParameterException" }),
    });

    await expect(
      createHandler(client, { metrics: h.metrics })(makeEvent()),
    ).rejects.toThrow();

    expect(h.lines).toHaveLength(1);
  });
});

// ── 所要時間 ──────────────────────────────────────────────────

describe("所要時間", () => {
  it("PublishLatency が出る", async () => {
    const h = harness();
    const { client } = makeMockSns({ onSend: () => h.advance(120) });

    await createHandler(client, { metrics: h.metrics })(makeEvent());

    expect(metricNames(h.last())).toContain("PublishLatency");
    expect(h.last().PublishLatency).toBe(120);
  });

  it("PublishLatency の単位は Milliseconds", async () => {
    const h = harness();
    const { client } = makeMockSns({ onSend: () => h.advance(5) });

    await createHandler(client, { metrics: h.metrics })(makeEvent());

    const defs = directiveOf(h.last()).Metrics as { Name: string; Unit: string }[];
    const latency = defs.find((d) => d.Name === "PublishLatency");
    expect(latency?.Unit).toBe("Milliseconds");
  });
});

// ── スキップ ──────────────────────────────────────────────────

describe("スキップした理由が残る", () => {
  it("SNS_TOPIC_ARN 未設定で EventSkipped が出る", async () => {
    delete process.env.SNS_TOPIC_ARN;
    const h = harness();
    const { client, calls } = makeMockSns();

    const result = await createHandler(client, { metrics: h.metrics })(makeEvent());

    expect(result.status).toBe("skipped");
    expect(calls).toHaveLength(0);
    expect(h.last().EventSkipped).toBe(1);
    expect(h.last().skipReason).toBe("missing-topic-arn");
  });

  it("必須フィールドの欠落で EventSkipped が出る", async () => {
    const h = harness();
    const { client } = makeMockSns();

    const result = await createHandler(client, { metrics: h.metrics })({
      source: "aws.ecs",
      "detail-type": "ECS Task State Change",
    } as unknown as EventBridgeEcsEvent);

    expect(result.status).toBe("skipped");
    expect(h.last().EventSkipped).toBe(1);
    expect(h.last().skipReason).toBe("missing-required-fields");
  });

  it("スキップ時は通知系のメトリクスを出さない", async () => {
    delete process.env.SNS_TOPIC_ARN;
    const h = harness();
    const { client } = makeMockSns();

    await createHandler(client, { metrics: h.metrics })(makeEvent());

    const names = metricNames(h.last());
    expect(names).not.toContain("NotificationSuccess");
    expect(names).not.toContain("PublishLatency");
  });
});

// ── 検証の警告 ────────────────────────────────────────────────

describe("検証の警告", () => {
  it("想定外の内容があると ValidationProblem が件数で出る", async () => {
    const h = harness();
    const { client } = makeMockSns();

    const result = await createHandler(client, { metrics: h.metrics })(
      makeEvent({ lastStatus: "UNKNOWN_STATUS" }),
    );

    // 通知は止めない。問題があったことだけ数える。
    expect(result.status).toBe("published");
    expect(h.last().ValidationProblem as number).toBeGreaterThanOrEqual(1);
  });

  it("問題がなければ ValidationProblem は出ない", async () => {
    const h = harness();
    const { client } = makeMockSns();

    await createHandler(client, { metrics: h.metrics })(makeEvent());

    expect(metricNames(h.last())).not.toContain("ValidationProblem");
  });
});

// ── 設計意図: 高カーディナリティをディメンションにしない ──────

describe("高カーディナリティの値をディメンションに入れない", () => {
  it("ディメンションは Handler だけ", async () => {
    const h = harness();
    const { client } = makeMockSns();

    await createHandler(client, { metrics: h.metrics })(makeEvent());

    expect(dimensionKeys(h.last())).toEqual(["Handler"]);
  });

  it("taskArn / clusterArn / lastStatus はプロパティとして出る", async () => {
    const h = harness();
    const { client } = makeMockSns();

    await createHandler(client, { metrics: h.metrics })(
      makeEvent({ lastStatus: "STOPPED" }),
    );

    const doc = h.last();
    expect(doc.lastStatus).toBe("STOPPED");
    expect(String(doc.clusterArn)).toContain("cluster/myapp-dev-cluster");
    expect(String(doc.taskArn)).toContain("task/myapp-dev-cluster/abc123");
    // プロパティはメトリクス定義に載らない（＝課金対象にならない）
    const names = metricNames(doc);
    expect(names).not.toContain("lastStatus");
    expect(names).not.toContain("taskArn");
  });
});

// ── 頑健性: メトリクスの失敗で通知を落とさない ────────────────

describe("メトリクスが壊れても通知は通る", () => {
  function brokenMetrics(): Metrics {
    const fail = () => {
      throw new Error("metrics is broken");
    };
    return {
      namespace: "Broken",
      putMetric: fail,
      count: fail,
      duration: fail,
      timer: () => fail,
      setDimensions: fail,
      setProperty: fail,
      flush: fail,
    };
  }

  it("count / setProperty が投げても published を返す", async () => {
    const { client, calls } = makeMockSns();

    const result = await createHandler(client, { metrics: brokenMetrics() })(makeEvent());

    expect(result.status).toBe("published");
    expect(calls).toHaveLength(1);
  });

  it("flush が投げても published を返す", async () => {
    const { client } = makeMockSns();
    const metrics: Metrics = {
      ...brokenMetrics(),
      count: () => undefined,
      setProperty: () => undefined,
      timer: () => () => undefined,
    };

    const result = await createHandler(client, { metrics })(makeEvent());

    expect(result.status).toBe("published");
  });
});

// ── EMF ドキュメントの形 ──────────────────────────────────────

describe("EMF ドキュメントの形", () => {
  it("1 回の呼び出しで 1 ドキュメントだけ出る", async () => {
    const h = harness();
    const { client } = makeMockSns();

    await createHandler(client, { metrics: h.metrics })(makeEvent());

    expect(h.lines).toHaveLength(1);
  });

  it("1 行に収まっている（CloudWatch Logs の前提）", async () => {
    const h = harness();
    const { client } = makeMockSns();

    await createHandler(client, { metrics: h.metrics })(makeEvent());

    expect(h.lines[0].includes("\n")).toBe(false);
  });
});

// ── 既定のメトリクス（注入しない場合） ────────────────────────

describe("注入しない場合は既定のメトリクスを使う", () => {
  it("console.log に EMF が出る（置いただけではない）", async () => {
    const spy = jest.spyOn(console, "log").mockImplementation();
    const { client } = makeMockSns();

    await createHandler(client)(makeEvent());

    const emf = spy.mock.calls.map((c) => String(c[0])).filter(isMetricsLine);
    expect(emf).toHaveLength(1);
    spy.mockRestore();
  });

  it("既定の名前空間は DEFAULT_METRICS_NAMESPACE", async () => {
    const spy = jest.spyOn(console, "log").mockImplementation();
    const { client } = makeMockSns();

    await createHandler(client)(makeEvent());

    const emf = spy.mock.calls.map((c) => String(c[0])).filter(isMetricsLine);
    const doc = JSON.parse(emf[0]) as Record<string, unknown>;
    expect(directiveOf(doc).Namespace).toBe(DEFAULT_METRICS_NAMESPACE);
    spy.mockRestore();
  });

  it("METRICS_NAMESPACE で名前空間を上書きできる", async () => {
    process.env.METRICS_NAMESPACE = "MyOwnNamespace";
    const spy = jest.spyOn(console, "log").mockImplementation();
    const { client } = makeMockSns();

    await createHandler(client)(makeEvent());

    const emf = spy.mock.calls.map((c) => String(c[0])).filter(isMetricsLine);
    const doc = JSON.parse(emf[0]) as Record<string, unknown>;
    expect(directiveOf(doc).Namespace).toBe("MyOwnNamespace");
    spy.mockRestore();
  });

  it("名前空間が空でも通知は通る（初期化の失敗を握りつぶす）", async () => {
    // 空文字は ?? を素通りするため createMetrics が例外を投げる経路に入る
    process.env.METRICS_NAMESPACE = "";
    const warnSpy = jest.spyOn(console, "warn").mockImplementation();
    const logSpy = jest.spyOn(console, "log").mockImplementation();
    const { client, calls } = makeMockSns();

    const result = await createHandler(client)(makeEvent());

    expect(result.status).toBe("published");
    expect(calls).toHaveLength(1);
    warnSpy.mockRestore();
    logSpy.mockRestore();
  });
});

// ── リトライとの結線 ──────────────────────────────────────────

describe("リトライがメトリクスにも伝わる", () => {
  it("リトライすると RetryAttempt が出る（ログと両方）", async () => {
    const h = harness();
    const warnings: string[] = [];
    const logger = createLogger({ sink: (line) => warnings.push(line) });

    let attempts = 0;
    const client: SnsPublisher = {
      send: async (_cmd: PublishCommand) => {
        attempts += 1;
        if (attempts === 1) {
          throw Object.assign(new Error("落ち着いて"), { name: "ThrottlingException" });
        }
        return { MessageId: "msg-retry", $metadata: {} };
      },
    };

    const result = await publishNotification(client, TOPIC_ARN, "件名", "本文", {
      logger,
      metrics: h.metrics,
      retry: { sleep: async () => undefined, rand: () => 0.5 },
    });
    h.metrics.flush();

    expect(result.status).toBe("published");
    expect(attempts).toBe(2);
    // メトリクス側
    expect(h.last().RetryAttempt).toBe(1);
    expect(h.last().retryOperation).toBe("Publish");
    // ログ側も消えていない
    expect(warnings.length).toBeGreaterThan(0);
  });
});
