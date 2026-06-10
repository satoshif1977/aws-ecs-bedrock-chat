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

// ── extractResourceName ───────────────────────────────────────────────────────

describe("extractResourceName", () => {
  test("クラスター ARN からクラスター名を取り出す", () => {
    expect(
      extractResourceName(
        "arn:aws:ecs:ap-northeast-1:123456789012:cluster/myapp-dev-cluster"
      )
    ).toBe("myapp-dev-cluster");
  });

  test("タスク ARN（スラッシュ複数）から末尾セグメントを取り出す", () => {
    expect(
      extractResourceName(
        "arn:aws:ecs:ap-northeast-1:123456789012:task/myapp-dev-cluster/abc123def456"
      )
    ).toBe("abc123def456");
  });

  test("スラッシュを含まない文字列はそのまま返す", () => {
    expect(extractResourceName("plain-name")).toBe("plain-name");
  });
});

// ── formatMessage ─────────────────────────────────────────────────────────────

describe("formatMessage", () => {
  test("クラスター名・タスク ID・ステータスを含む", () => {
    const msg = formatMessage(makeDetail());
    expect(msg).toContain("myapp-dev-cluster");
    expect(msg).toContain("abc123def456");
    expect(msg).toContain("RUNNING");
  });

  test("group が指定されているときサービス行を含む", () => {
    const msg = formatMessage(makeDetail({ group: "service:myapp-dev-app" }));
    expect(msg).toContain("service:myapp-dev-app");
  });

  test("stoppedReason が指定されているとき停止理由行を含む", () => {
    const msg = formatMessage(
      makeDetail({
        lastStatus: "STOPPED",
        stoppedReason: "Essential container in task exited",
      })
    );
    expect(msg).toContain("Essential container in task exited");
  });

  test("group も stoppedReason もない場合は余分な行を含まない", () => {
    const msg = formatMessage(makeDetail());
    expect(msg).not.toContain("サービス");
    expect(msg).not.toContain("停止理由");
  });

  test("全フィールドを含むメッセージを正しく生成する", () => {
    const msg = formatMessage(
      makeDetail({
        lastStatus: "STOPPED",
        group: "service:myapp-svc",
        stoppedReason: "Task failed ELB health checks",
      })
    );
    expect(msg).toContain("myapp-dev-cluster");
    expect(msg).toContain("STOPPED");
    expect(msg).toContain("service:myapp-svc");
    expect(msg).toContain("Task failed ELB health checks");
  });

  test("複数行で構成されている", () => {
    const msg = formatMessage(makeDetail());
    expect(msg.split("\n").length).toBeGreaterThanOrEqual(4);
  });
});

// ── buildSubject ──────────────────────────────────────────────────────────────

describe("buildSubject", () => {
  test("RUNNING は OK ラベルを含む", () => {
    const subj = buildSubject("RUNNING");
    expect(subj).toContain("OK");
    expect(subj).toContain("RUNNING");
  });

  test("STOPPED は ALERT ラベルを含む", () => {
    expect(buildSubject("STOPPED")).toContain("ALERT");
  });

  test("PROVISIONING は INFO ラベルを含む", () => {
    expect(buildSubject("PROVISIONING")).toContain("INFO");
  });

  test("DEPROVISIONING は INFO ラベルを含む", () => {
    expect(buildSubject("DEPROVISIONING")).toContain("INFO");
  });

  test("未知のステータスは INFO ラベルにフォールバックする", () => {
    expect(buildSubject("ACTIVATING")).toContain("INFO");
  });

  test("件名にステータス文字列が含まれる", () => {
    expect(buildSubject("STOPPED")).toContain("STOPPED");
  });
});

// ── publishNotification ───────────────────────────────────────────────────────

describe("publishNotification", () => {
  test("status=published と messageId を返す", async () => {
    const { client } = makeMockSns("msg-abc");
    const result = await publishNotification(
      client,
      "arn:aws:sns:ap-northeast-1:123:test-topic",
      "件名",
      "本文"
    );
    expect(result.status).toBe("published");
    expect(result.messageId).toBe("msg-abc");
  });

  test("SNS クライアントが 1 回呼び出される", async () => {
    const { client, calls } = makeMockSns();
    await publishNotification(
      client,
      "arn:aws:sns:ap-northeast-1:123:test-topic",
      "件名",
      "本文"
    );
    expect(calls).toHaveLength(1);
  });

  test("SNS エラー時は例外を伝播する", async () => {
    const errorClient: SnsPublisher = {
      send: async () => {
        throw new Error("SNS unavailable");
      },
    };
    await expect(
      publishNotification(errorClient, "arn:aws:sns:::topic", "件名", "本文")
    ).rejects.toThrow("SNS unavailable");
  });
});

// ── createHandler / handler ───────────────────────────────────────────────────

describe("createHandler", () => {
  afterEach(() => {
    delete process.env.SNS_TOPIC_ARN;
  });

  test("SNS_TOPIC_ARN 未設定のとき skipped を返す", async () => {
    const { client } = makeMockSns();
    const h = createHandler(client);
    const result = await h(makeEvent());
    expect(result.status).toBe("skipped");
  });

  test("SNS_TOPIC_ARN 設定済みのとき published を返す", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:test-topic";
    const { client } = makeMockSns("msg-xyz");
    const h = createHandler(client);
    const result = await h(makeEvent());
    expect(result.status).toBe("published");
    expect(result.messageId).toBe("msg-xyz");
  });

  test("SNS が 1 回呼ばれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:test-topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent());
    expect(calls).toHaveLength(1);
  });

  test("STOPPED イベントを正しく処理する", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:test-topic";
    const { client } = makeMockSns();
    const h = createHandler(client);
    const result = await h(
      makeEvent({ lastStatus: "STOPPED", stoppedReason: "OOMKilled" })
    );
    expect(result.status).toBe("published");
  });

  test("SNS エラー時は例外が伝播する", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:test-topic";
    const errorClient: SnsPublisher = {
      send: async () => {
        throw new Error("connection refused");
      },
    };
    const h = createHandler(errorClient);
    await expect(h(makeEvent())).rejects.toThrow("connection refused");
  });
});
