# kube-postcheck

[![ci](https://github.com/OTA-EITA/kube-postcheck/actions/workflows/ci.yml/badge.svg)](https://github.com/OTA-EITA/kube-postcheck/actions/workflows/ci.yml)

Kubernetes クラスタの構築直後や変更作業の後に、「本当に正常か」を一発で確認するための CLI です。
ヘルスチェックとセキュリティチェックをまとめて実行し、問題があれば終了コード 1 を返すので、CI や作業手順書のゲートとしてそのまま使えます。

A single-binary CLI that verifies a Kubernetes cluster right after it is built or changed.
It runs health and security checks and exits non-zero when something needs attention, so it can gate CI pipelines and runbooks.

## なぜ作ったか

クラスタの構築やバージョンアップの後は、ノード・ワークロード・ストレージ・証明書・権限などを毎回 kubectl で目視確認していました。
確認項目が人によってぶれる、作業の証跡が残りにくい、という課題があったため、確認を 1 コマンドにまとめ、結果を Markdown で証跡として残せるようにしました。

## チェック項目

| 名前 | 内容 | FAIL になる例 | WARN になる例 |
|---|---|---|---|
| `nodes` | ノードの状態 | NotReady | DiskPressure などの圧迫状態、cordon 中、kubelet のバージョン差 |
| `workloads` | Deployment / StatefulSet / DaemonSet | 利用可能なレプリカが 0 | 一部のレプリカのみ利用可能 |
| `pods` | Pod の異常 | CrashLoopBackOff、ImagePullBackOff、Pending の滞留、Failed | 再起動回数がしきい値以上 |
| `storage` | PVC と StorageClass | PVC が Pending / Lost | デフォルト StorageClass が 0 個または複数 |
| `services` | LoadBalancer Service | 外部アドレスが未割り当て | - |
| `certificates` | `kubernetes.io/tls` Secret の証明書 | 期限切れ、壊れた証明書 | 期限まで 30 日以内 |
| `pod-security` | Pod のセキュリティ設定 | privileged、hostNetwork / hostPID / hostIPC | hostPath、allowPrivilegeEscalation 未無効化、root 実行の可能性 |
| `rbac` | cluster-admin の付与先 | ServiceAccount への付与 | system: 以外のユーザー・グループへの付与 |

`pod-security` は CNI や CSI など正当にホスト権限が必要なものを除くため、既定で `kube-system` を対象外にしています（`--security-exempt-namespace` で変更可能）。

## インストール

[Releases](https://github.com/OTA-EITA/kube-postcheck/releases) から OS に合ったバイナリをダウンロードするか、Go でインストールします。

```sh
go install github.com/OTA-EITA/kube-postcheck/cmd/kube-postcheck@latest
```

コンテナイメージもあります（distroless、非 root で動作）。

```sh
docker run --rm -v ~/.kube:/home/nonroot/.kube:ro ghcr.io/ota-eita/kube-postcheck:latest --help
```

またはソースからビルドします。

```sh
git clone https://github.com/OTA-EITA/kube-postcheck.git
cd kube-postcheck
make build   # bin/kube-postcheck
```

## 使い方

```sh
# 現在の kubeconfig のコンテキストに対して全チェックを実行
kube-postcheck

# 問題のあるものだけ表示
kube-postcheck --quiet

# 対象の namespace を絞り、WARN でも失敗扱いにする
kube-postcheck --namespace app,monitoring --fail-on warn

# 作業の証跡として Markdown で保存
kube-postcheck --context prod --output markdown > postcheck-$(date +%Y%m%d).md

# 特定のチェックだけ / 特定のチェックを除外
kube-postcheck --only nodes,workloads
kube-postcheck --skip certificates
```

出力例（`--quiet`）:

```
STATUS  CHECK         RESOURCE                     MESSAGE
FAIL    pods          pod/app/api-7d9f-x2k         container api: CrashLoopBackOff
FAIL    rbac          clusterrolebinding/ci-admin  cluster-admin granted to ServiceAccount ci/deployer
WARN    workloads     deployment/app/api           1/2 available
WARN    pod-security  pod/app/api-7d9f-x2k         allowPrivilegeEscalation not false in api, may run as root: api

overall=FAIL pass=6 warn=2 fail=2 errors=0
```

### 主なオプション

| フラグ | 既定値 | 説明 |
|---|---|---|
| `--kubeconfig` / `--context` | `$KUBECONFIG` / 現在のコンテキスト | 接続先 |
| `--output` | `table` | `table` / `json` / `markdown` |
| `--fail-on` | `fail` | `warn` にすると WARN でも終了コード 1 |
| `--namespace` / `--exclude-namespace` | 全 namespace | 対象の絞り込み（カンマ区切り、複数指定可） |
| `--only` / `--skip` | - | 実行するチェックの選択 |
| `--restart-threshold` | `5` | 再起動回数の警告しきい値（0 で無効） |
| `--pending-grace` | `5m` | Pending を異常とみなすまでの時間 |
| `--cert-warn-days` | `30` | 証明書期限の警告日数 |
| `--security-exempt-namespace` | `kube-system` | `pod-security` の対象外 namespace |
| `--timeout` | `60s` | API 呼び出し全体のタイムアウト |
| `--list` | - | チェック一覧を表示 |

### 終了コード

| コード | 意味 |
|---|---|
| 0 | 問題なし（`--fail-on warn` でない限り WARN を含む） |
| 1 | FAIL がある、またはチェックが実行できなかった |
| 2 | 引数や kubeconfig の誤り |

## 必要な権限

すべて読み取り（list）のみです。cluster-admin を使わずに済むよう、最小権限の ServiceAccount を [`deploy/rbac.yaml`](deploy/rbac.yaml) に用意しています。
`certificates` チェックのために Secret の list 権限が必要です。付与したくない場合はその行を削除し、`--skip certificates` で実行してください。

## CI での利用例

```yaml
- name: Post-build check
  run: kube-postcheck --output markdown --fail-on warn | tee -a "$GITHUB_STEP_SUMMARY"
```

このリポジトリ自身の CI でも、[kind](https://kind.sigs.k8s.io/) で作ったクラスタに対して実行しています（`.github/workflows/ci.yml` の `e2e` ジョブ）。

## 開発

```sh
make test   # go test -race
make lint   # golangci-lint
make cover  # カバレッジ
```

各チェックは `client-go` の fake clientset を使ったテーブルテストで検証しているため、実クラスタなしでテストできます。
新しいチェックは `check.Checker` インターフェースを実装し、`internal/check/registry.go` に追加します。

## License

MIT
