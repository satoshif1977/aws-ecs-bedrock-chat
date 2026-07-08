import {
  extractResourceName,
  formatMessage,
  buildSubject,
  publishNotification,
  createHandler,
  EcsTaskDetail,
  SnsPublisher,
} from "./index";
import { PublishCommand } from "@aws-sdk/client-sns";

// ── テスト用ヘルパー ──────────────────────────────────────────────────────────

const makeDetail = (overrides: Partial<EcsTaskDetail> = {}): EcsTaskDetail => ({
  clusterArn:
    "arn:aws:ecs:ap-northeast-1:123456789012:cluster/myapp-dev-cluster",
  taskArn:
    "arn:aws:ecs:ap-northeast-1:123456789012:task/myapp-dev-cluster/abc123def456",
  lastStatus: "RUNNING",
  desiredStatus: "RUNNING",
  ...overrides,
});

const makeMockSns = (
  messageId = "msg-0001"
): { client: SnsPublisher; calls: PublishCommand[] } => {
  const calls: PublishCommand[] = [];
  const client: SnsPublisher = {
    send: async (cmd: PublishCommand) => {
      calls.push(cmd);
      return { MessageId: messageId, $metadata: {} };
    },
  };
  return { client, calls };
};

const makeEvent = (overrides: Partial<EcsTaskDetail> = {}) => ({
  source: "aws.ecs",
  "detail-type": "ECS Task State Change",
  detail: makeDetail(overrides),
});

// ── extractResourceName 詳細 ──────────────────────────────────────────────────

describe("extractResourceName / 詳細", () => {
  test("空文字列を渡したとき空文字列を返す", () => {
    expect(extractResourceName("")).toBe("");
  });

  test("スラッシュのみの文字列は空文字列を返す", () => {
    expect(extractResourceName("/")).toBe("");
  });

  test("3 階層 ARN から末尾セグメントを返す", () => {
    expect(
      extractResourceName(
        "arn:aws:ecs:ap-northeast-1:123:task/cluster/taskid999"
      )
    ).toBe("taskid999");
  });
});

// ── formatMessage 詳細 ────────────────────────────────────────────────────────

describe("formatMessage / 詳細", () => {
  test("desiredStatus はメッセージ本文に含まれない", () => {
    const msg = formatMessage(makeDetail({ desiredStatus: "STOPPED" }));
    expect(msg).not.toContain("desiredStatus");
    expect(msg).not.toContain("STOPPED");
  });

  test("「ステータス :」ラベルを含む", () => {
    const msg = formatMessage(makeDetail());
    expect(msg).toContain("ステータス");
  });

  test("group と stoppedReason が両方あるとき両方の行が含まれる", () => {
    const msg = formatMessage(
      makeDetail({
        group: "service:myapp-prod",
        stoppedReason: "Exit code 1",
      })
    );
    expect(msg).toContain("service:myapp-prod");
    expect(msg).toContain("Exit code 1");
  });

  test("taskArn から取り出したタスク ID が含まれる", () => {
    const msg = formatMessage(
      makeDetail({
        taskArn:
          "arn:aws:ecs:ap-northeast-1:123:task/cluster/unique-task-id-xyz",
      })
    );
    expect(msg).toContain("unique-task-id-xyz");
  });
});

// ── buildSubject 詳細 ─────────────────────────────────────────────────────────

describe("buildSubject / 詳細", () => {
  test("RUNNING の件名に RUNNING が含まれる", () => {
    expect(buildSubject("RUNNING")).toContain("RUNNING");
  });

  test("DEPROVISIONING の件名に DEPROVISIONING が含まれる", () => {
    expect(buildSubject("DEPROVISIONING")).toContain("DEPROVISIONING");
  });

  test("件名が文字列で返る", () => {
    expect(typeof buildSubject("RUNNING")).toBe("string");
  });
});

// ── publishNotification 詳細 ──────────────────────────────────────────────────

describe("publishNotification / 詳細", () => {
  test("SNS コマンドに正しい Message が設定される", async () => {
    const { client, calls } = makeMockSns();
    await publishNotification(
      client,
      "arn:aws:sns:ap-northeast-1:123:topic",
      "件名",
      "テスト本文"
    );
    expect(calls[0].input.Message).toBe("テスト本文");
  });

  test("別の messageId が正しく返される", async () => {
    const { client } = makeMockSns("msg-unique-999");
    const result = await publishNotification(
      client,
      "arn:aws:sns:ap-northeast-1:123:topic",
      "件名",
      "本文"
    );
    expect(result.messageId).toBe("msg-unique-999");
  });

  test("連続して 2 回呼び出しても正しく動作する", async () => {
    const { client, calls } = makeMockSns();
    await publishNotification(client, "arn:::", "件名1", "本文1");
    await publishNotification(client, "arn:::", "件名2", "本文2");
    expect(calls).toHaveLength(2);
    expect(calls[0].input.Subject).toBe("件名1");
    expect(calls[1].input.Subject).toBe("件名2");
  });
});

// ── createHandler 詳細 ────────────────────────────────────────────────────────

describe("createHandler / 詳細", () => {
  afterEach(() => {
    delete process.env.SNS_TOPIC_ARN;
  });

  test("RUNNING イベントで SNS subject に OK が含まれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent({ lastStatus: "RUNNING" }));
    expect(calls[0].input.Subject).toContain("OK");
  });

  test("STOPPED イベントで SNS subject に ALERT が含まれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent({ lastStatus: "STOPPED" }));
    expect(calls[0].input.Subject).toContain("ALERT");
  });

  test("group ありイベントで SNS message に group が含まれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent({ group: "service:prod-svc" }));
    expect(calls[0].input.Message).toContain("service:prod-svc");
  });

  test("stoppedReason ありイベントで SNS message に reason が含まれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent({ lastStatus: "STOPPED", stoppedReason: "OOMKilled" }));
    expect(calls[0].input.Message).toContain("OOMKilled");
  });

  test("DEPROVISIONING イベントを正しく処理する", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client } = makeMockSns();
    const h = createHandler(client);
    const result = await h(makeEvent({ lastStatus: "DEPROVISIONING" }));
    expect(result.status).toBe("published");
  });

  test("STOPPING イベントを正しく処理する", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client } = makeMockSns();
    const h = createHandler(client);
    const result = await h(makeEvent({ lastStatus: "STOPPING" }));
    expect(result.status).toBe("published");
  });
});
