/**
 * ハンドラーが validators.ts を「実際に経由している」ことを固定する。
 *
 * ユーティリティを置いただけで呼び出し元に結線していない、という欠陥は
 * カバレッジでは検出できない（validators.test.ts は単体で通ってしまう）。
 * ここではハンドラーを実行し、壊れたイベントで落ちないこと・
 * フォーマット不正では通知を止めないことの両方を観測する。
 */

import { createHandler, formatMessage } from "./index";
import type { EcsTaskDetail, EventBridgeEcsEvent, SnsPublisher } from "./index";
import { createLogger } from "./logger";
import type { Logger } from "./logger";
import { PublishCommand } from "@aws-sdk/client-sns";

const TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123456789012:ecs-notify";

const makeDetail = (overrides: Partial<EcsTaskDetail> = {}): EcsTaskDetail => ({
  clusterArn: "arn:aws:ecs:ap-northeast-1:123456789012:cluster/myapp-dev-cluster",
  taskArn:
    "arn:aws:ecs:ap-northeast-1:123456789012:task/myapp-dev-cluster/abc123def456",
  lastStatus: "RUNNING",
  desiredStatus: "RUNNING",
  ...overrides,
});

const makeEvent = (overrides: Partial<EcsTaskDetail> = {}): EventBridgeEcsEvent => ({
  source: "aws.ecs",
  "detail-type": "ECS Task State Change",
  detail: makeDetail(overrides),
});

const makeMockSns = (): { client: SnsPublisher; calls: PublishCommand[] } => {
  const calls: PublishCommand[] = [];
  const client: SnsPublisher = {
    send: async (cmd: PublishCommand) => {
      calls.push(cmd);
      return { MessageId: "msg-0001", $metadata: {} };
    },
  };
  return { client, calls };
};

/** sink は JSON 1 行を受け取るので、パースして貯める */
function collector(): { logger: Logger; entries: Record<string, unknown>[] } {
  const entries: Record<string, unknown>[] = [];
  const logger = createLogger({
    level: "debug",
    sink: (line: string) => {
      entries.push(JSON.parse(line) as Record<string, unknown>);
    },
  });
  return { logger, entries };
}

const levels = (entries: Record<string, unknown>[], level: string) =>
  entries.filter((e) => e.level === level);

describe("ハンドラーと validators の結線", () => {
  beforeEach(() => {
    process.env.SNS_TOPIC_ARN = TOPIC_ARN;
  });
  afterEach(() => {
    delete process.env.SNS_TOPIC_ARN;
  });

  // ── 検証が無いと落ちる、という前提の確認 ──

  test("前提: detail が無いまま formatMessage に渡すと TypeError で落ちる", () => {
    expect(() => formatMessage(undefined as unknown as EcsTaskDetail)).toThrow(
      TypeError
    );
  });

  test("前提: taskArn が無いまま formatMessage に渡すと TypeError で落ちる", () => {
    const broken = { ...makeDetail(), taskArn: undefined } as unknown as EcsTaskDetail;
    expect(() => formatMessage(broken)).toThrow(TypeError);
  });

  // ── 必須フィールド欠落 → 通知せず skipped（落ちない） ──

  test("detail ごと無いイベントでも落ちず skipped を返す", async () => {
    const { logger, entries } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const result = await h({
      source: "aws.ecs",
      "detail-type": "ECS Task State Change",
    } as unknown as EventBridgeEcsEvent);

    expect(result.status).toBe("skipped");
    expect(result.reason).toContain("detail");
    expect(calls).toHaveLength(0);

    const errors = levels(entries, "error");
    expect(errors).toHaveLength(1);
    expect(errors[0].missingFields).toEqual(["detail"]);
  });

  test("taskArn が欠けていたら通知せず skipped を返す", async () => {
    const { logger, entries } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const event = makeEvent();
    delete (event.detail as Partial<EcsTaskDetail>).taskArn;

    const result = await h(event);

    expect(result.status).toBe("skipped");
    expect(result.reason).toContain("detail.taskArn");
    expect(calls).toHaveLength(0);
    expect(levels(entries, "error")[0].missingFields).toEqual(["detail.taskArn"]);
  });

  test("clusterArn が欠けていたら通知せず skipped を返す", async () => {
    const { logger } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const event = makeEvent();
    delete (event.detail as Partial<EcsTaskDetail>).clusterArn;

    expect((await h(event)).status).toBe("skipped");
    expect(calls).toHaveLength(0);
  });

  test("lastStatus が欠けていたら通知せず skipped を返す", async () => {
    const { logger } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const event = makeEvent();
    delete (event.detail as Partial<EcsTaskDetail>).lastStatus;

    expect((await h(event)).status).toBe("skipped");
    expect(calls).toHaveLength(0);
  });

  test("欠落が複数あれば理由にすべて並ぶ", async () => {
    const { logger, entries } = collector();
    const { client } = makeMockSns();
    const h = createHandler(client, { logger });

    const event = makeEvent();
    delete (event.detail as Partial<EcsTaskDetail>).taskArn;
    delete (event.detail as Partial<EcsTaskDetail>).lastStatus;

    const result = await h(event);

    expect(result.reason).toContain("detail.taskArn");
    expect(result.reason).toContain("detail.lastStatus");
    expect(levels(entries, "error")[0].missingFields).toEqual([
      "detail.taskArn",
      "detail.lastStatus",
    ]);
  });

  // ── フォーマット不正 → 警告は出すが通知は止めない ──

  test("ARN のフォーマットが不正でも通知は送る（warn のみ）", async () => {
    const { logger, entries } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const result = await h(
      makeEvent({ clusterArn: "arn:aws:ecs:ap-northeast-1:123:cluster/short-account" })
    );

    expect(result.status).toBe("published");
    expect(calls).toHaveLength(1);

    const warns = levels(entries, "warn");
    expect(warns).toHaveLength(1);
    expect(warns[0].problems).toContain("detail.clusterArn");
    expect(levels(entries, "error")).toHaveLength(0);
  });

  test("想定外のステータスでも通知は送る", async () => {
    const { logger, entries } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const result = await h(makeEvent({ lastStatus: "UNKNOWN_STATUS" }));

    expect(result.status).toBe("published");
    expect(calls).toHaveLength(1);
    expect(levels(entries, "warn")).toHaveLength(1);
  });

  test("STOPPED で stoppedReason が無ければ warn を出しつつ通知する", async () => {
    const { logger, entries } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const result = await h(makeEvent({ lastStatus: "STOPPED" }));

    expect(result.status).toBe("published");
    expect(calls).toHaveLength(1);
    expect(levels(entries, "warn")[0].problems).toContain("stoppedReason");
  });

  test("source が ECS 以外でも通知は送る（warn のみ）", async () => {
    const { logger, entries } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const event = { ...makeEvent(), source: "aws.ec2" };
    const result = await h(event);

    expect(result.status).toBe("published");
    expect(calls).toHaveLength(1);
    expect(levels(entries, "warn")[0].problems).toContain("source");
  });

  // ── 正常系 ──

  test("正常なイベントでは警告を出さず通知する", async () => {
    const { logger, entries } = collector();
    const { client, calls } = makeMockSns();
    const h = createHandler(client, { logger });

    const result = await h(
      makeEvent({ lastStatus: "RUNNING", group: "service:api-svc" })
    );

    expect(result.status).toBe("published");
    expect(calls).toHaveLength(1);
    expect(levels(entries, "warn")).toHaveLength(0);
    expect(levels(entries, "error")).toHaveLength(0);
  });

  test("ロガーを渡さなくても落ちない（既定ロガーが使われる）", async () => {
    const warnSpy = jest.spyOn(console, "warn").mockImplementation(() => {});
    const logSpy = jest.spyOn(console, "log").mockImplementation(() => {});
    try {
      const { client, calls } = makeMockSns();
      const h = createHandler(client);

      const result = await h(makeEvent());

      expect(result.status).toBe("published");
      expect(calls).toHaveLength(1);
    } finally {
      warnSpy.mockRestore();
      logSpy.mockRestore();
    }
  });

  test("SNS_TOPIC_ARN が無いときは検証より前にスキップする", async () => {
    delete process.env.SNS_TOPIC_ARN;
    const { logger, entries } = collector();
    const { client, calls } = makeMockSns();
    const warnSpy = jest.spyOn(console, "warn").mockImplementation(() => {});
    try {
      const h = createHandler(client, { logger });

      // detail ごと壊れていても、検証に入る前にスキップされる
      const result = await h({} as unknown as EventBridgeEcsEvent);

      expect(result.status).toBe("skipped");
      expect(result.reason).toBeUndefined();
      expect(calls).toHaveLength(0);
      expect(entries).toHaveLength(0);
    } finally {
      warnSpy.mockRestore();
    }
  });
});
