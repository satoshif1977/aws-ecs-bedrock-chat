# ── ベースイメージ ────────────────────────────
# Python 3.11 の軽量版（slim）を使う
FROM python:3.11-slim

# ── 作業ディレクトリ ──────────────────────────
# コンテナ内での作業場所を /app に設定
WORKDIR /app

# ── 依存パッケージのインストール ──────────────
# requirements.txt だけ先にコピーしてインストール
# （app.py を変えてもこのレイヤーはキャッシュされるため高速）
COPY app/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# ── アプリのコピー ────────────────────────────
COPY app/ .

# ── ポート公開 ────────────────────────────────
# Streamlit のデフォルトポート 8501 を開ける
EXPOSE 8501

# ── 起動コマンド ──────────────────────────────
# コンテナ起動時に Streamlit を立ち上げる
ENTRYPOINT ["streamlit", "run", "app.py", \
            "--server.port=8501", \
            "--server.address=0.0.0.0"]
