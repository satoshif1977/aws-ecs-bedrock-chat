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

// ── buildSubject / 正確なフォーマット検証 ────────────────────────────────────

describe("buildSubject / 正確なフォーマット（test.each）", () => {
  test.each([
    ["RUNNING", "[ECS OK] タスク RUNNING"],
    ["STOPPED", "[ECS ALERT] タスク STOPPED"],
    ["PROVISIONING", "[ECS INFO] タスク PROVISIONING"],
    ["DEPROVISIONING", "[ECS INFO] タスク DEPROVISIONING"],
    ["PENDING", "[ECS INFO] タスク PENDING"],
    ["STOPPING", "[ECS INFO] タスク STOPPING"],
    ["ACTIVATING", "[ECS INFO] タスク ACTIVATING"],
  ])("buildSubject(%s) === %s", (status, expected) => {
    expect(buildSubject(status)).toBe(expected);
  });

  test("空文字列ステータスでも正しいフォーマットを返す", () => {
    expect(buildSubject("")).toBe("[ECS INFO] タスク ");
  });
});

// ── formatMessage / 行単位の正確な内容検証 ───────────────────────────────────

describe("formatMessage / 行構造", () => {
  test("基本4行: ヘッダー・クラスター・タスクID・ステータス", () => {
    const msg = formatMessage(makeDetail());
    const lines = msg.split("\n");
    expect(lines[0]).toBe("ECS タスク状態変更");
    expect(lines[1]).toBe("クラスター : myapp-dev-cluster");
    expect(lines[2]).toBe("タスク ID  : abc123def456");
    expect(lines[3]).toBe("ステータス : RUNNING");
  });

  test("group ありのとき5行目にサービス行が追加される", () => {
    const msg = formatMessage(makeDetail({ group: "service:web-app" }));
    const lines = msg.split("\n");
    expect(lines).toHaveLength(5);
    expect(lines[4]).toBe("サービス   : service:web-app");
  });

  test("stoppedReason のみのとき5行目に停止理由行が追加される", () => {
    const msg = formatMessage(
      makeDetail({ stoppedReason: "Essential container exited" })
    );
    const lines = msg.split("\n");
    expect(lines).toHaveLength(5);
    expect(lines[4]).toBe("停止理由   : Essential container exited");
  });

  test("group + stoppedReason の順序が正しい（サービス→停止理由）", () => {
    const msg = formatMessage(
      makeDetail({
        group: "service:api",
        stoppedReason: "OOMKilled",
        lastStatus: "STOPPED",
      })
    );
    const lines = msg.split("\n");
    expect(lines).toHaveLength(6);
    expect(lines[4]).toBe("サービス   : service:api");
    expect(lines[5]).toBe("停止理由   : OOMKilled");
  });

  test("STOPPED ステータスが3行目に正しく表示される", () => {
    const msg = formatMessage(makeDetail({ lastStatus: "STOPPED" }));
    expect(msg.split("\n")[3]).toBe("ステータス : STOPPED");
  });
});

// ── extractResourceName / エッジケース ───────────────────────────────────────

describe("extractResourceName / エッジケース", () => {
  test("連続スラッシュを含む場合は最後のセグメントを返す", () => {
    expect(extractResourceName("a//b///c")).toBe("c");
  });

  test("連続スラッシュのみは空文字を返す", () => {
    expect(extractResourceName("///")).toBe("");
  });

  test("コロン区切りの ARN プレフィックスはスラッシュで分割される", () => {
    expect(
      extractResourceName("arn:aws:ecs:region:account:service/resource-name")
    ).toBe("resource-name");
  });

  test("特殊文字（ハイフン・アンダースコア・ドット）を含むリソース名", () => {
    expect(extractResourceName("prefix/my_task-id.v2")).toBe("my_task-id.v2");
  });

  test("数字のみのリソース名", () => {
    expect(extractResourceName("cluster/12345")).toBe("12345");
  });
});

// ── publishNotification / PublishCommand 構造検証 ────────────────────────────

describe("publishNotification / コマンド構造", () => {
  test("PublishCommand の3フィールドが全て正しく設定される", async () => {
    const { client, calls } = makeMockSns();
    const arn = "arn:aws:sns:ap-northeast-1:123:my-topic";
    await publishNotification(client, arn, "テスト件名", "テスト本文");

    expect(calls).toHaveLength(1);
    const input = calls[0].input;
    expect(input.TopicArn).toBe(arn);
    expect(input.Subject).toBe("テスト件名");
    expect(input.Message).toBe("テスト本文");
  });

  test("長い件名がそのまま渡される（切り詰めなし）", async () => {
    const { client, calls } = makeMockSns();
    const longSubject = "A".repeat(200);
    await publishNotification(client, "arn:::", longSubject, "本文");
    expect(calls[0].input.Subject).toBe(longSubject);
    expect(calls[0].input.Subject).toHaveLength(200);
  });

  test("空の件名・本文でもエラーにならない", async () => {
    const { client } = makeMockSns();
    const result = await publishNotification(client, "arn:::", "", "");
    expect(result.status).toBe("published");
  });

  test("NotificationResult に status と messageId のみ含まれる", async () => {
    const { client } = makeMockSns("msg-check");
    const result = await publishNotification(client, "arn:::", "s", "m");
    expect(Object.keys(result).sort()).toEqual(["messageId", "status"]);
  });
});

// ── createHandler / 統合テスト（Subject + Message + TopicArn 一括検証）──────

describe("createHandler / 統合テスト", () => {
  afterEach(() => {
    delete process.env.SNS_TOPIC_ARN;
  });

  test("RUNNING: Subject・Message・TopicArn が全て正しい", async () => {
    const topicArn = "arn:aws:sns:ap-northeast-1:123:prod-topic";
    process.env.SNS_TOPIC_ARN = topicArn;
    const { client, calls } = makeMockSns("msg-integration");
    const h = createHandler(client);

    const result = await h(
      makeEvent({
        clusterArn: "arn:aws:ecs:ap-northeast-1:123:cluster/prod-cluster",
        taskArn: "arn:aws:ecs:ap-northeast-1:123:task/prod-cluster/task-xyz",
        lastStatus: "RUNNING",
        group: "service:api-svc",
      })
    );

    expect(result.status).toBe("published");
    expect(result.messageId).toBe("msg-integration");

    const input = calls[0].input;
    expect(input.TopicArn).toBe(topicArn);
    expect(input.Subject).toBe("[ECS OK] タスク RUNNING");

    const msg = input.Message!;
    expect(msg).toContain("prod-cluster");
    expect(msg).toContain("task-xyz");
    expect(msg).toContain("RUNNING");
    expect(msg).toContain("service:api-svc");
  });

  test("STOPPED + stoppedReason: Subject=ALERT・Message に理由含む", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:alert-topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);

    await h(
      makeEvent({
        lastStatus: "STOPPED",
        stoppedReason: "Essential container in task exited",
        group: "service:worker",
      })
    );

    const input = calls[0].input;
    expect(input.Subject).toBe("[ECS ALERT] タスク STOPPED");
    expect(input.Message).toContain("Essential container in task exited");
    expect(input.Message).toContain("service:worker");
  });

  test("PROVISIONING: Subject=INFO・Message にステータス含む", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:info-topic";
    const { client, calls } = makeMockSns();
    const h = createHandler(client);

    await h(makeEvent({ lastStatus: "PROVISIONING" }));

    expect(calls[0].input.Subject).toBe("[ECS INFO] タスク PROVISIONING");
    expect(calls[0].input.Message).toContain("PROVISIONING");
  });
});

// ── createHandler / console 出力検証 ─────────────────────────────────────────

describe("createHandler / console 出力", () => {
  afterEach(() => {
    delete process.env.SNS_TOPIC_ARN;
  });

  test("published 時の console.log に Subject 内容が含まれる", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const logSpy = jest.spyOn(console, "log").mockImplementation();
    const { client } = makeMockSns();
    const h = createHandler(client);

    await h(makeEvent({ lastStatus: "STOPPED" }));

    expect(logSpy).toHaveBeenCalledWith("[ECS Notifier] [ECS ALERT] タスク STOPPED");
    logSpy.mockRestore();
  });

  test("skipped 時は console.log ではなく console.warn が呼ばれる", async () => {
    delete process.env.SNS_TOPIC_ARN;
    const logSpy = jest.spyOn(console, "log").mockImplementation();
    const warnSpy = jest.spyOn(console, "warn").mockImplementation();
    const { client } = makeMockSns();
    const h = createHandler(client);

    await h(makeEvent());

    expect(warnSpy).toHaveBeenCalled();
    expect(logSpy).not.toHaveBeenCalled();
    logSpy.mockRestore();
    warnSpy.mockRestore();
  });

  test("skipped 時は SNS client が呼ばれない", async () => {
    delete process.env.SNS_TOPIC_ARN;
    const { client, calls } = makeMockSns();
    const h = createHandler(client);

    await h(makeEvent());

    expect(calls).toHaveLength(0);
  });
});

// ── createHandler / 異なる client インスタンスの独立性 ────────────────────────

describe("createHandler / ファクトリの独立性", () => {
  afterEach(() => {
    delete process.env.SNS_TOPIC_ARN;
  });

  test("異なるファクトリからのハンドラーは独立した SNS client を使う", async () => {
    process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
    const mock1 = makeMockSns("from-handler1");
    const mock2 = makeMockSns("from-handler2");

    const h1 = createHandler(mock1.client);
    const h2 = createHandler(mock2.client);

    const r1 = await h1(makeEvent({ lastStatus: "RUNNING" }));
    const r2 = await h2(makeEvent({ lastStatus: "STOPPED" }));

    expect(r1.messageId).toBe("from-handler1");
    expect(r2.messageId).toBe("from-handler2");
    expect(mock1.calls).toHaveLength(1);
    expect(mock2.calls).toHaveLength(1);
    expect(mock1.calls[0].input.Subject).toContain("OK");
    expect(mock2.calls[0].input.Subject).toContain("ALERT");
  });
});

// ── 全 ECS ステータスの Subject + Message 一括検証 ───────────────────────────

describe("全ステータス一括検証（test.each）", () => {
  afterEach(() => {
    delete process.env.SNS_TOPIC_ARN;
  });

  test.each([
    ["RUNNING", "OK"],
    ["STOPPED", "ALERT"],
    ["PROVISIONING", "INFO"],
    ["DEPROVISIONING", "INFO"],
    ["PENDING", "INFO"],
    ["STOPPING", "INFO"],
    ["ACTIVATING", "INFO"],
  ])(
    "lastStatus=%s → Subject に %s ラベル・Message に %s ステータス",
    async (status, label) => {
      process.env.SNS_TOPIC_ARN = "arn:aws:sns:ap-northeast-1:123:topic";
      const { client, calls } = makeMockSns();
      const h = createHandler(client);

      const result = await h(makeEvent({ lastStatus: status }));

      expect(result.status).toBe("published");
      expect(calls[0].input.Subject).toBe(`[ECS ${label}] タスク ${status}`);
      expect(calls[0].input.Message).toContain(status);
    }
  );
});

// ── formatMessage / 全フィールド組み合わせ ───────────────────────────────────

describe("formatMessage / フィールド組み合わせ test.each", () => {
  test.each([
    [{ group: undefined, stoppedReason: undefined }, 4],
    [{ group: "service:web", stoppedReason: undefined }, 5],
    [{ group: undefined, stoppedReason: "OOM" }, 5],
    [{ group: "service:api", stoppedReason: "Exit 1" }, 6],
  ] as [Partial<EcsTaskDetail>, number][])(
    "overrides=%j → %d 行",
    (overrides, expectedLines) => {
      const msg = formatMessage(makeDetail(overrides));
      expect(msg.split("\n")).toHaveLength(expectedLines);
    }
  );
});
