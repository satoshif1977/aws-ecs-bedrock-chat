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

  test("スラッシュを含まない文字列はそのまま返す", () => {
    expect(extractResourceName("simple-string")).toBe("simple-string");
  });

  test("末尾がスラッシュの場合は空文字列を返す", () => {
    expect(extractResourceName("arn:aws:ecs/")).toBe("");
  });

  test("4 階層パスから末尾セグメントを返す", () => {
    expect(extractResourceName("a/b/c/deepest")).toBe("deepest");
  });

  test("日本語を含む ARN でも末尾セグメントを返す", () => {
    expect(extractResourceName("prefix/テスト名")).toBe("テスト名");
  });

  test("UUID 形式のタスク ID を正しく取り出す", () => {
    expect(
      extractResourceName(
        "arn:aws:ecs:us-east-1:999:task/prod-cluster/550e8400-e29b-41d4-a716-446655440000"
      )
    ).toBe("550e8400-e29b-41d4-a716-446655440000");
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

  test("group なし・stoppedReason なしのとき 4 行で構成される", () => {
    const msg = formatMessage(makeDetail());
    expect(msg.split("\n")).toHaveLength(4);
  });

  test("group ありのとき 5 行で構成される", () => {
    const msg = formatMessage(makeDetail({ group: "service:web" }));
    expect(msg.split("\n")).toHaveLength(5);
  });

  test("group + stoppedReason ありのとき 6 行で構成される", () => {
    const msg = formatMessage(
      makeDetail({ group: "service:web", stoppedReason: "OOM" })
    );
    expect(msg.split("\n")).toHaveLength(6);
  });

  test("先頭行が「ECS タスク状態変更」で始まる", () => {
    const msg = formatMessage(makeDetail());
    expect(msg.split("\n")[0]).toBe("ECS タスク状態変更");
  });

  test("clusterArn からクラスター名を抽出して表示する", () => {
    const msg = formatMessage(
      makeDetail({
        clusterArn: "arn:aws:ecs:us-west-2:999:cluster/prod-app-cluster",
      })
    );
    expect(msg).toContain("prod-app-cluster");
  });

  test("stoppedReason のみ（group なし）のとき 5 行で構成される", () => {
    const msg = formatMessage(makeDetail({ stoppedReason: "Scaling down" }));
    expect(msg.split("\n")).toHaveLength(5);
    expect(msg).toContain("Scaling down");
  });

  test("lastStatus が PENDING のとき正しく表示される", () => {
    const msg = formatMessage(makeDetail({ lastStatus: "PENDING" }));
    expect(msg).toContain("PENDING");
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

  test("PROVISIONING の件名に INFO ラベルが含まれる", () => {
    expect(buildSubject("PROVISIONING")).toContain("INFO");
  });

  test("STOPPED の件名に ALERT ラベルが含まれる", () => {
    expect(buildSubject("STOPPED")).toContain("ALERT");
  });

  test("未知のステータスは INFO ラベルにフォールバックする", () => {
    expect(buildSubject("UNKNOWN_STATUS")).toContain("INFO");
  });

  test("件名が [ECS で始まる", () => {
    expect(buildSubject("RUNNING")).toMatch(/^\[ECS /);
  });

  test("件名に「タスク」が含まれる", () => {
    expect(buildSubject("STOPPED")).toContain("タスク");
  });

  test("PENDING は INFO ラベルにフォールバックする", () => {
    expect(buildSubject("PENDING")).toContain("INFO");
    expect(buildSubject("PENDING")).toContain("PENDING");
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

  test("TopicArn が正しくコマンドに設定される", async () => {
    const { client, calls } = makeMockSns();
    const arn = "arn:aws:sns:ap-northeast-1:123:my-topic";
    await publishNotification(client, arn, "件名", "本文");
    expect(calls[0].input.TopicArn).toBe(arn);
  });

  test("SNS エラーが呼び出し元に伝播する", async () => {
    const client: SnsPublisher = {
      send: async () => {
        throw new Error("SNS publish failed");
      },
    };
    await expect(
      publishNotification(client, "arn:::", "件名", "本文")
    ).rejects.toThrow("SNS publish failed");
  });

  test("status が published で返される", async () => {
    const { client } = makeMockSns();
    const result = await publishNotification(client, "arn:::", "件名", "本文");
    expect(result.status).toBe("published");
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

  test("SNS_TOPIC_ARN 未設定時に console.warn が呼ばれる", async () => {
    delete process.env.SNS_TOPIC_ARN;
    const warnSpy = jest.spyOn(console, "warn").mockImplementation();
    const { client } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent());
    expect(warnSpy).toHaveBeenCalledWith(
      expect.stringContaining("SNS_TOPIC_ARN")
    );
    warnSpy.mockRestore();
  });

  test("正常イベントで console.log が呼ばれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const logSpy = jest.spyOn(console, "log").mockImplementation();
    const { client } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent({ lastStatus: "RUNNING" }));
    expect(logSpy).toHaveBeenCalledWith(
      expect.stringContaining("[ECS Notifier]")
    );
    logSpy.mockRestore();
  });

  test("PROVISIONING イベントで subject に INFO が含まれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent({ lastStatus: "PROVISIONING" }));
    expect(calls[0].input.Subject).toContain("INFO");
  });

  test("SNS エラーがハンドラーから伝播する", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const client: SnsPublisher = {
      send: async () => {
        throw new Error("SNS timeout");
      },
    };
    const h = createHandler(client);
    await expect(h(makeEvent())).rejects.toThrow("SNS timeout");
  });

  test("messageId が結果に含まれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client } = makeMockSns("msg-detail-001");
    const h = createHandler(client);
    const result = await h(makeEvent());
    expect(result.messageId).toBe("msg-detail-001");
  });

  test("2 つのイベントを連続処理しても各回で正しく SNS に送信される", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent({ lastStatus: "RUNNING" }));
    await h(makeEvent({ lastStatus: "STOPPED" }));
    expect(calls).toHaveLength(2);
    expect(calls[0].input.Subject).toContain("OK");
    expect(calls[1].input.Subject).toContain("ALERT");
  });

  test("PENDING イベントで subject に INFO が含まれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);
    await h(makeEvent({ lastStatus: "PENDING" }));
    expect(calls[0].input.Subject).toContain("INFO");
  });
});
