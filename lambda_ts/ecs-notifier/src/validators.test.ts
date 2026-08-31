import {
  // 型
  ValidationError,
  EcsEventInput,
  // 定数
  VALID_TASK_STATUSES,
  ALERT_STATUSES,
  INFO_STATUSES,
  ECS_EVENT_SOURCE,
  ECS_TASK_STATE_CHANGE,
  CLUSTER_ARN_PATTERN,
  TASK_ARN_PATTERN,
  SERVICE_GROUP_PATTERN,
  MAX_SNS_SUBJECT_BYTES,
  EXIT_CODE_OOM_KILL,
  EXIT_CODE_SUCCESS,
  // ARN
  isValidClusterArn,
  isValidTaskArn,
  extractRegion,
  extractAccountId,
  // ステータス
  isValidTaskStatus,
  isAlertStatus,
  isValidStatusTransition,
  // 終了コード
  classifyExitCode,
  isSuccessExitCode,
  // サービスグループ
  isValidServiceGroup,
  extractServiceName,
  // SNS Subject
  isValidSnsSubject,
  getSnsSubjectByteLength,
  // イベント
  validateEcsEvent,
  // ユーティリティ
  hasErrors,
  formatErrors,
} from "./validators";

// ── テストヘルパー ────────────────────────────────────────────

const VALID_CLUSTER_ARN =
  "arn:aws:ecs:ap-northeast-1:123456789012:cluster/my-cluster";
const VALID_TASK_ARN =
  "arn:aws:ecs:ap-northeast-1:123456789012:task/my-cluster/abc123def456";

function validEvent(overrides?: Partial<EcsEventInput>): EcsEventInput {
  return {
    source: ECS_EVENT_SOURCE,
    "detail-type": ECS_TASK_STATE_CHANGE,
    detail: {
      clusterArn: VALID_CLUSTER_ARN,
      taskArn: VALID_TASK_ARN,
      lastStatus: "RUNNING",
      desiredStatus: "RUNNING",
      group: "service:my-service",
    },
    ...overrides,
  };
}

function errorsOnly(errors: ValidationError[]): ValidationError[] {
  return errors.filter((e) => e.severity === "error");
}

function warningsOnly(errors: ValidationError[]): ValidationError[] {
  return errors.filter((e) => e.severity === "warning");
}

// ── 定数 ─────────────────────────────────────────────────────

describe("定数", () => {
  test("VALID_TASK_STATUSES は 8 種類", () => {
    expect(VALID_TASK_STATUSES).toHaveLength(8);
    expect(VALID_TASK_STATUSES).toContain("RUNNING");
    expect(VALID_TASK_STATUSES).toContain("STOPPED");
  });

  test("ALERT_STATUSES は STOPPED を含む", () => {
    expect(ALERT_STATUSES).toContain("STOPPED");
  });

  test("INFO_STATUSES は PROVISIONING を含む", () => {
    expect(INFO_STATUSES).toContain("PROVISIONING");
    expect(INFO_STATUSES).not.toContain("RUNNING");
    expect(INFO_STATUSES).not.toContain("STOPPED");
  });

  test("ECS_EVENT_SOURCE は aws.ecs", () => {
    expect(ECS_EVENT_SOURCE).toBe("aws.ecs");
  });

  test("ECS_TASK_STATE_CHANGE は正しい文字列", () => {
    expect(ECS_TASK_STATE_CHANGE).toBe("ECS Task State Change");
  });

  test("MAX_SNS_SUBJECT_BYTES は 256", () => {
    expect(MAX_SNS_SUBJECT_BYTES).toBe(256);
  });

  test("EXIT_CODE_OOM_KILL は 137", () => {
    expect(EXIT_CODE_OOM_KILL).toBe(137);
  });

  test("EXIT_CODE_SUCCESS は 0", () => {
    expect(EXIT_CODE_SUCCESS).toBe(0);
  });
});

// ── isValidClusterArn ────────────────────────────────────────

describe("isValidClusterArn", () => {
  test("正常な ARN", () => {
    expect(isValidClusterArn(VALID_CLUSTER_ARN)).toBe(true);
  });

  test("us-east-1 リージョン", () => {
    expect(
      isValidClusterArn(
        "arn:aws:ecs:us-east-1:123456789012:cluster/prod-cluster"
      )
    ).toBe(true);
  });

  test("ハイフン付きクラスター名", () => {
    expect(
      isValidClusterArn(
        "arn:aws:ecs:ap-northeast-1:123456789012:cluster/my-ecs-cluster-01"
      )
    ).toBe(true);
  });

  test("空文字は無効", () => {
    expect(isValidClusterArn("")).toBe(false);
  });

  test("不正な ARN（cluster/ なし）", () => {
    expect(
      isValidClusterArn("arn:aws:ecs:ap-northeast-1:123456789012:my-cluster")
    ).toBe(false);
  });

  test("アカウント ID が 12 桁でない", () => {
    expect(
      isValidClusterArn("arn:aws:ecs:ap-northeast-1:1234:cluster/my-cluster")
    ).toBe(false);
  });

  test("タスク ARN はクラスター ARN として無効", () => {
    expect(isValidClusterArn(VALID_TASK_ARN)).toBe(false);
  });
});

// ── isValidTaskArn ───────────────────────────────────────────

describe("isValidTaskArn", () => {
  test("正常な ARN", () => {
    expect(isValidTaskArn(VALID_TASK_ARN)).toBe(true);
  });

  test("長い hex タスク ID", () => {
    expect(
      isValidTaskArn(
        "arn:aws:ecs:us-west-2:123456789012:task/my-cluster/abcdef0123456789"
      )
    ).toBe(true);
  });

  test("空文字は無効", () => {
    expect(isValidTaskArn("")).toBe(false);
  });

  test("クラスター ARN はタスク ARN として無効", () => {
    expect(isValidTaskArn(VALID_CLUSTER_ARN)).toBe(false);
  });

  test("task/ のみ（タスク ID なし）", () => {
    expect(
      isValidTaskArn(
        "arn:aws:ecs:ap-northeast-1:123456789012:task/my-cluster/"
      )
    ).toBe(false);
  });
});

// ── extractRegion ────────────────────────────────────────────

describe("extractRegion", () => {
  test("クラスター ARN からリージョン抽出", () => {
    expect(extractRegion(VALID_CLUSTER_ARN)).toBe("ap-northeast-1");
  });

  test("タスク ARN からリージョン抽出", () => {
    expect(extractRegion(VALID_TASK_ARN)).toBe("ap-northeast-1");
  });

  test("us-east-1 抽出", () => {
    expect(
      extractRegion("arn:aws:ecs:us-east-1:123456789012:cluster/x")
    ).toBe("us-east-1");
  });

  test("無効な ARN は空文字", () => {
    expect(extractRegion("invalid")).toBe("");
  });

  test("空文字は空文字", () => {
    expect(extractRegion("")).toBe("");
  });
});

// ── extractAccountId ─────────────────────────────────────────

describe("extractAccountId", () => {
  test("正常な ARN からアカウント ID 抽出", () => {
    expect(extractAccountId(VALID_CLUSTER_ARN)).toBe("123456789012");
  });

  test("異なるアカウント ID", () => {
    expect(
      extractAccountId(
        "arn:aws:ecs:ap-northeast-1:987654321098:cluster/my-cluster"
      )
    ).toBe("987654321098");
  });

  test("無効な ARN は空文字", () => {
    expect(extractAccountId("not-an-arn")).toBe("");
  });
});

// ── isValidTaskStatus ────────────────────────────────────────

describe("isValidTaskStatus", () => {
  test.each([
    "PROVISIONING",
    "PENDING",
    "ACTIVATING",
    "RUNNING",
    "DEACTIVATING",
    "STOPPING",
    "DEPROVISIONING",
    "STOPPED",
  ])('"%s" は有効', (s) => {
    expect(isValidTaskStatus(s)).toBe(true);
  });

  test.each(["running", "Running", "UNKNOWN", "FAILED", ""])(
    '"%s" は無効',
    (s) => {
      expect(isValidTaskStatus(s)).toBe(false);
    }
  );
});

// ── isAlertStatus ────────────────────────────────────────────

describe("isAlertStatus", () => {
  test("STOPPED はアラート", () => {
    expect(isAlertStatus("STOPPED")).toBe(true);
  });

  test("RUNNING はアラートでない", () => {
    expect(isAlertStatus("RUNNING")).toBe(false);
  });

  test("PROVISIONING はアラートでない", () => {
    expect(isAlertStatus("PROVISIONING")).toBe(false);
  });
});

// ── isValidStatusTransition ──────────────────────────────────

describe("isValidStatusTransition", () => {
  test("PROVISIONING → RUNNING は有効", () => {
    expect(isValidStatusTransition("PROVISIONING", "RUNNING")).toBe(true);
  });

  test("RUNNING → RUNNING は有効", () => {
    expect(isValidStatusTransition("RUNNING", "RUNNING")).toBe(true);
  });

  test("RUNNING → STOPPED は有効", () => {
    expect(isValidStatusTransition("RUNNING", "STOPPED")).toBe(true);
  });

  test("STOPPING → STOPPED は有効", () => {
    expect(isValidStatusTransition("STOPPING", "STOPPED")).toBe(true);
  });

  test("STOPPED → RUNNING は無効（逆方向）", () => {
    expect(isValidStatusTransition("STOPPED", "RUNNING")).toBe(false);
  });

  test("RUNNING → PROVISIONING は無効（desiredStatus が中間値）", () => {
    expect(isValidStatusTransition("RUNNING", "PROVISIONING")).toBe(false);
  });

  test("INVALID → RUNNING は無効", () => {
    expect(isValidStatusTransition("INVALID", "RUNNING")).toBe(false);
  });

  test("RUNNING → INVALID は無効", () => {
    expect(isValidStatusTransition("RUNNING", "INVALID")).toBe(false);
  });
});

// ── classifyExitCode ─────────────────────────────────────────

describe("classifyExitCode", () => {
  test("0 は success", () => {
    expect(classifyExitCode(0)).toBe("success");
  });

  test("137 は oom_kill", () => {
    expect(classifyExitCode(137)).toBe("oom_kill");
  });

  test("1 は error", () => {
    expect(classifyExitCode(1)).toBe("error");
  });

  test("255 は error", () => {
    expect(classifyExitCode(255)).toBe("error");
  });

  test("-1 は error", () => {
    expect(classifyExitCode(-1)).toBe("error");
  });
});

// ── isSuccessExitCode ────────────────────────────────────────

describe("isSuccessExitCode", () => {
  test("0 は正常", () => {
    expect(isSuccessExitCode(0)).toBe(true);
  });

  test("1 は異常", () => {
    expect(isSuccessExitCode(1)).toBe(false);
  });

  test("137 は異常", () => {
    expect(isSuccessExitCode(137)).toBe(false);
  });
});

// ── isValidServiceGroup ──────────────────────────────────────

describe("isValidServiceGroup", () => {
  test("service:my-service は有効", () => {
    expect(isValidServiceGroup("service:my-service")).toBe(true);
  });

  test("service:web_app は有効", () => {
    expect(isValidServiceGroup("service:web_app")).toBe(true);
  });

  test("service: のみは無効", () => {
    expect(isValidServiceGroup("service:")).toBe(false);
  });

  test("プレフィックスなしは無効", () => {
    expect(isValidServiceGroup("my-service")).toBe(false);
  });

  test("空文字は無効", () => {
    expect(isValidServiceGroup("")).toBe(false);
  });
});

// ── extractServiceName ───────────────────────────────────────

describe("extractServiceName", () => {
  test("正常なグループからサービス名抽出", () => {
    expect(extractServiceName("service:my-service")).toBe("my-service");
  });

  test("無効なグループは空文字", () => {
    expect(extractServiceName("invalid")).toBe("");
  });

  test("空文字は空文字", () => {
    expect(extractServiceName("")).toBe("");
  });
});

// ── isValidSnsSubject ────────────────────────────────────────

describe("isValidSnsSubject", () => {
  test("通常の Subject は有効", () => {
    expect(isValidSnsSubject("[ECS OK] タスク RUNNING")).toBe(true);
  });

  test("空文字は無効", () => {
    expect(isValidSnsSubject("")).toBe(false);
  });

  test("256 バイトちょうどは有効", () => {
    const subject = "a".repeat(256);
    expect(isValidSnsSubject(subject)).toBe(true);
  });

  test("257 バイトは無効", () => {
    const subject = "a".repeat(257);
    expect(isValidSnsSubject(subject)).toBe(false);
  });

  test("日本語（マルチバイト）でバイト数チェック", () => {
    // 「あ」は UTF-8 で 3 バイト → 86 文字で 258 バイト
    const subject = "あ".repeat(86);
    expect(isValidSnsSubject(subject)).toBe(false);
  });
});

// ── getSnsSubjectByteLength ──────────────────────────────────

describe("getSnsSubjectByteLength", () => {
  test("ASCII は 1 バイト/文字", () => {
    expect(getSnsSubjectByteLength("hello")).toBe(5);
  });

  test("日本語は 3 バイト/文字", () => {
    expect(getSnsSubjectByteLength("あ")).toBe(3);
  });

  test("空文字は 0", () => {
    expect(getSnsSubjectByteLength("")).toBe(0);
  });

  test("混合文字列", () => {
    // "ECSあ" = E(1) + C(1) + S(1) + あ(3) = 6
    expect(getSnsSubjectByteLength("ECSあ")).toBe(6);
  });
});

// ── validateEcsEvent ─────────────────────────────────────────

describe("validateEcsEvent", () => {
  test("正常なイベントはエラーなし", () => {
    expect(validateEcsEvent(validEvent())).toHaveLength(0);
  });

  test("source が不正は error", () => {
    const result = errorsOnly(
      validateEcsEvent(validEvent({ source: "aws.ec2" }))
    );
    expect(result.some((e) => e.field === "source")).toBe(true);
  });

  test("detail-type が不正は error", () => {
    const result = errorsOnly(
      validateEcsEvent(validEvent({ "detail-type": "wrong" }))
    );
    expect(result.some((e) => e.field === "detail-type")).toBe(true);
  });

  test("detail が undefined は error", () => {
    const result = errorsOnly(
      validateEcsEvent({ source: ECS_EVENT_SOURCE, "detail-type": ECS_TASK_STATE_CHANGE })
    );
    expect(result.some((e) => e.field === "detail")).toBe(true);
  });

  test("clusterArn が undefined は error", () => {
    const event = validEvent();
    delete event.detail!.clusterArn;
    const result = errorsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail.clusterArn")).toBe(true);
  });

  test("clusterArn が不正フォーマットは error", () => {
    const event = validEvent();
    event.detail!.clusterArn = "invalid-arn";
    const result = errorsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail.clusterArn")).toBe(true);
  });

  test("taskArn が undefined は error", () => {
    const event = validEvent();
    delete event.detail!.taskArn;
    const result = errorsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail.taskArn")).toBe(true);
  });

  test("lastStatus が不正は error", () => {
    const event = validEvent();
    event.detail!.lastStatus = "INVALID";
    const result = errorsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail.lastStatus")).toBe(true);
  });

  test("desiredStatus が不正は error", () => {
    const event = validEvent();
    event.detail!.desiredStatus = "UNKNOWN";
    const result = errorsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail.desiredStatus")).toBe(true);
  });

  test("リージョン不一致は error", () => {
    const event = validEvent();
    event.detail!.taskArn =
      "arn:aws:ecs:us-east-1:123456789012:task/my-cluster/abc123";
    const result = errorsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail")).toBe(true);
  });

  test("アカウント ID 不一致は error", () => {
    const event = validEvent();
    event.detail!.taskArn =
      "arn:aws:ecs:ap-northeast-1:999999999999:task/my-cluster/abc123";
    const result = errorsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail")).toBe(true);
  });

  test("不正なステータス遷移は warning", () => {
    const event = validEvent();
    event.detail!.lastStatus = "STOPPED";
    event.detail!.desiredStatus = "RUNNING";
    const result = warningsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail")).toBe(true);
  });

  test("無効な group は warning", () => {
    const event = validEvent();
    event.detail!.group = "invalid-group";
    const result = warningsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail.group")).toBe(true);
  });

  test("STOPPED + stoppedReason なしは warning", () => {
    const event = validEvent();
    event.detail!.lastStatus = "STOPPED";
    event.detail!.desiredStatus = "STOPPED";
    delete event.detail!.stoppedReason;
    const result = warningsOnly(validateEcsEvent(event));
    expect(result.some((e) => e.field === "detail.stoppedReason")).toBe(true);
  });

  test("STOPPED + stoppedReason ありは warning なし（stoppedReason）", () => {
    const event = validEvent();
    event.detail!.lastStatus = "STOPPED";
    event.detail!.desiredStatus = "STOPPED";
    event.detail!.stoppedReason = "Essential container exited";
    const result = warningsOnly(validateEcsEvent(event));
    expect(result.every((e) => e.field !== "detail.stoppedReason")).toBe(true);
  });

  test("group 未指定は warning なし（オプショナル）", () => {
    const event = validEvent();
    delete event.detail!.group;
    const result = warningsOnly(validateEcsEvent(event));
    expect(result.every((e) => e.field !== "detail.group")).toBe(true);
  });
});

// ── hasErrors / formatErrors ─────────────────────────────────

describe("hasErrors", () => {
  test("error ありは true", () => {
    const errors: ValidationError[] = [
      { field: "x", message: "err", severity: "error" },
    ];
    expect(hasErrors(errors)).toBe(true);
  });

  test("warning のみは false", () => {
    const errors: ValidationError[] = [
      { field: "x", message: "warn", severity: "warning" },
    ];
    expect(hasErrors(errors)).toBe(false);
  });

  test("空配列は false", () => {
    expect(hasErrors([])).toBe(false);
  });
});

describe("formatErrors", () => {
  test("空配列は通過メッセージ", () => {
    expect(formatErrors([])).toBe("すべてのチェックが通過しました");
  });

  test("error のフォーマット", () => {
    const errors: ValidationError[] = [
      { field: "source", message: "テスト", severity: "error" },
    ];
    expect(formatErrors(errors)).toBe("[ERROR] source: テスト");
  });

  test("複数件は改行区切り", () => {
    const errors: ValidationError[] = [
      { field: "a", message: "e1", severity: "error" },
      { field: "b", message: "w1", severity: "warning" },
    ];
    const formatted = formatErrors(errors);
    expect(formatted.split("\n")).toHaveLength(2);
  });
});
