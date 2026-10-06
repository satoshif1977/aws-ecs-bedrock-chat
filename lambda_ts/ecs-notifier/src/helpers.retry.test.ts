/**
 * publishNotification が retry.ts を「実際に経由している」ことを固定する。
 *
 * ユーティリティを置いただけで呼び出し元に結線していない、という欠陥は
 * カバレッジでは検出できない（retry.ts 単体のテストは通ってしまう）。
 * ここでは publishNotification 経由で SNS を叩き、リトライ回数・
 * ログフックの発火・成功時の非発火を観測する。
 */

import { publishNotification, RETRY_OPERATION } from "./helpers";
import { createLogger } from "./logger";
import type { SnsPublisher } from "./types";

// 実待機ゼロ・ジッター無しのリトライ設定
const fastRetry = (maxAttempts: number) => ({
  config: { maxAttempts, baseDelayMs: 1, maxDelayMs: 1, jitter: false },
  sleep: async () => {},
});

/** AWS SDK が返すスロットリングエラーを模す */
function throttling(): Error {
  const e = new Error("Rate exceeded") as Error & { name: string };
  e.name = "ThrottlingException";
  return e;
}

/** リトライ不能なエラーを模す */
function validationError(): Error {
  const e = new Error("invalid parameter") as Error & { name: string };
  e.name = "ValidationException";
  return e;
}

/** sink は JSON 1 行を受け取るので、パースして貯める */
function collectingLogger() {
  const entries: Record<string, unknown>[] = [];
  const logger = createLogger({
    level: "debug",
    sink: (line: string) => {
      entries.push(JSON.parse(line) as Record<string, unknown>);
    },
  });
  return { logger, entries };
}

const ARN = "arn:aws:sns:ap-northeast-1:123456789012:ecs-notify";

describe("publishNotification と retry の結線", () => {
  test("スロットリングならリトライして最終的に成功する", async () => {
    let calls = 0;
    const client: SnsPublisher = {
      send: async () => {
        calls++;
        if (calls < 3) throw throttling();
        return { MessageId: "msg-retried" } as never;
      },
    };

    const result = await publishNotification(client, ARN, "件名", "本文", {
      retry: fastRetry(4),
    });

    expect(calls).toBe(3); // 2 回リトライして 3 回目で成功
    expect(result).toEqual({ status: "published", messageId: "msg-retried" });
  });

  test("リトライ不能なエラーは即座に投げ直す", async () => {
    let calls = 0;
    const client: SnsPublisher = {
      send: async () => {
        calls++;
        throw validationError();
      },
    };

    await expect(
      publishNotification(client, ARN, "件名", "本文", { retry: fastRetry(4) })
    ).rejects.toThrow("invalid parameter");

    expect(calls).toBe(1); // リトライしない
  });

  test("最大試行回数を使い切ったら元のエラーを投げる", async () => {
    let calls = 0;
    const client: SnsPublisher = {
      send: async () => {
        calls++;
        throw throttling();
      },
    };

    await expect(
      publishNotification(client, ARN, "件名", "本文", { retry: fastRetry(3) })
    ).rejects.toThrow("Rate exceeded");

    expect(calls).toBe(3);
  });

  test("リトライ時に retryLogger が warn を出す", async () => {
    const { logger, entries } = collectingLogger();
    let calls = 0;
    const client: SnsPublisher = {
      send: async () => {
        calls++;
        if (calls < 2) throw throttling();
        return { MessageId: "msg-hook" } as never;
      },
    };

    await publishNotification(client, ARN, "件名", "本文", {
      logger,
      retry: fastRetry(3),
    });

    const warns = entries.filter((e) => e.level === "warn");
    expect(warns).toHaveLength(1);
    expect(warns[0].operation).toBe(RETRY_OPERATION);
    expect(warns[0].attempt).toBe(1);
    expect(typeof warns[0].delayMs).toBe("number");
  });

  test("成功時はリトライのログを出さず、成功ログに messageId を残す", async () => {
    const { logger, entries } = collectingLogger();
    const client: SnsPublisher = {
      send: async () => ({ MessageId: "msg-ok" }) as never,
    };

    await publishNotification(client, ARN, "件名", "本文", {
      logger,
      retry: fastRetry(3),
    });

    expect(entries.filter((e) => e.level === "warn")).toHaveLength(0);

    const infos = entries.filter((e) => e.level === "info");
    expect(infos).toHaveLength(1);
    expect(infos[0].messageId).toBe("msg-ok");
    expect(infos[0].operation).toBe(RETRY_OPERATION);
  });

  test("ログに機密情報が出ないよう component が付く", async () => {
    const { logger, entries } = collectingLogger();
    const client: SnsPublisher = {
      send: async () => ({ MessageId: "msg-child" }) as never,
    };

    // logger を渡した場合は呼び出し側の設定をそのまま使う
    await publishNotification(client, ARN, "件名", "本文", {
      logger: logger.child({ component: "ecs-notifier" }),
      retry: fastRetry(2),
    });

    const infos = entries.filter((e) => e.level === "info");
    expect(infos[0].component).toBe("ecs-notifier");
  });
});
