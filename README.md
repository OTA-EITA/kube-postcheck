# kube-postcheck

[![ci](https://github.com/OTA-EITA/kube-postcheck/actions/workflows/ci.yml/badge.svg)](https://github.com/OTA-EITA/kube-postcheck/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/OTA-EITA/kube-postcheck/badge)](https://securityscorecards.dev/viewer/?uri=github.com/OTA-EITA/kube-postcheck)

Kubernetes クラスタの構築直後や変更作業の後に、「本当に正常か」を一発で確認するための CLI です。
ヘルスチェックとセキュリティチェックをまとめて実行し、問題があれば終了コード 1 を返すので、CI や作業手順書のゲートとしてそのまま使えます。
`kube-postcheck score` で、ワークロードを本番運用のベストプラクティスで採点することもできます（[ワークロードの採点](#ワークロードの採点score)）。

以前の [kube-readiness](https://github.com/OTA-EITA/kube-readiness) はこのリポジトリに統合しました。

A single-binary CLI that verifies a Kubernetes cluster right after it is built or changed.
It runs health and security checks and exits non-zero when something needs attention, so it can gate CI pipelines and runbooks.
`kube-postcheck score` also scores workloads for production readiness (reliability, efficiency, security) from manifests or a live cluster, in the spirit of Polaris.

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

## ワークロードの採点（score）

`kube-postcheck score` は、Kubernetes のワークロードを**本番運用のベストプラクティス**（信頼性・効率・セキュリティ）で採点します。Polaris と同じ考え方で、マニフェスト（ファイル、ディレクトリ、`helm template` の出力）と稼働中のクラスタの両方に使えます。
クラスタ全体の健康状態を見る通常のチェックに対して、こちらはワークロード 1 つ 1 つの設定の良し悪しを点数にします。

以前の [kube-readiness](https://github.com/OTA-EITA/kube-readiness) はこのリポジトリに統合しました。チェックの ID（REL-001 など）、設定ファイルの形式、`kube-readiness/ignore` アノテーションはそのまま使えます。コマンドは `kube-readiness ...` を `kube-postcheck score ...` に置き換えてください。

- **デプロイ前（CI）と稼働中（クラスタ）の両方**: 同じチェックで、PR のマニフェストも本番クラスタも採点できます
- **アップデートを止める PDB を検出**: `minAvailable` がレプリカ数と同じ、`maxUnavailable: 0` など、ノードのドレインを永遠に待たせる PDB を danger として扱います（[kube-upgrade-preflight](https://github.com/OTA-EITA/kube-upgrade-preflight) と同じ観点を、デプロイ前に止めます）
- **SARIF 出力でマニフェストの行を指す**: GitHub の Code scanning に取り込むと、PR の該当マニフェストに注釈が付きます
- **スコアとグレード**: ワークロードごと、カテゴリごと、全体の点数（A〜F）を出し、`--min-score` で CI のゲートにできます

```sh
# マニフェストを採点（ディレクトリは *.yaml / *.yml を再帰的に読む）
kube-postcheck score k8s/

# Helm チャートを採点
helm template my-release ./chart | kube-postcheck score -

# 稼働中のクラスタを採点（kube-system などは既定で除外）
kube-postcheck score --cluster --context prod

# CI のゲート: 80 点未満か danger があれば終了コード 1
kube-postcheck score --min-score 80 --fail-on-danger k8s/

# GitHub の Code scanning 向け
kube-postcheck score --output sarif k8s/ > score.sarif
```

出力例（[`examples/`](examples) に対する実行結果）:

```
SCORE  GRADE  WORKLOAD                            FAILED CHECKS
12     F      DaemonSet/readiness-bad/node-agent  SEC-001! SEC-002! SEC-007! REL-001 REL-002 EFF-001 EFF-002 EFF-003 SEC-003 SEC-004 SEC-005 SEC-006 SEC-008 SEC-009 SEC-010
40     F      Deployment/readiness-bad/api        REL-005! REL-007! REL-001 REL-002 REL-006 EFF-001 EFF-002 EFF-003 SEC-003 SEC-004 SEC-005 SEC-006 SEC-009 SEC-010
41     F      StatefulSet/readiness-bad/cache     REL-007! REL-001 REL-002 REL-003 EFF-001 EFF-002 EFF-003 SEC-003 SEC-004 SEC-006 SEC-009 SEC-010
100    A      Deployment/readiness-good/web       -

Score 50 (F)  reliability 48  efficiency 25  security 57  |  4 workloads, 6 danger, 35 warning
(! = danger. Run with --output markdown for details.)
```

[`examples/good.yaml`](examples/good.yaml) は全チェックに合格し、実際に kind で動く構成です。新しいワークロードの雛形としても使えます。

### 採点のチェック

| ID | カテゴリ | 重大度 | 内容 |
|---|---|---|---|
| REL-001 | reliability | warning | readinessProbe がある |
| REL-002 | reliability | warning | livenessProbe がある |
| REL-003 | reliability | warning | レプリカが 2 以上 |
| REL-004 | reliability | warning | PodDisruptionBudget で守られている |
| REL-005 | reliability | **danger** | PDB が少なくとも 1 台の停止を許す（ドレインが止まらない） |
| REL-006 | reliability | warning | topologySpreadConstraints か podAntiAffinity でノードに分散している |
| REL-007 | reliability | **danger** | イメージがタグ固定（`latest` やタグなしではない）かダイジェスト指定 |
| EFF-001 | efficiency | warning | CPU の request がある |
| EFF-002 | efficiency | warning | メモリの request がある |
| EFF-003 | efficiency | warning | メモリの limit がある |
| SEC-001 | security | **danger** | privileged ではない |
| SEC-002 | security | **danger** | hostNetwork / hostPID / hostIPC を使わない |
| SEC-003 | security | warning | root 以外で動く |
| SEC-004 | security | warning | allowPrivilegeEscalation が false |
| SEC-005 | security | warning | ルートファイルシステムが読み取り専用 |
| SEC-006 | security | warning | capabilities を ALL で drop している |
| SEC-007 | security | **danger** | SYS_ADMIN や NET_ADMIN などの危険な capability を追加しない |
| SEC-008 | security | warning | hostPath ボリュームを使わない |
| SEC-009 | security | warning | seccomp プロファイルが RuntimeDefault か Localhost |
| SEC-010 | security | warning | ServiceAccount のトークンを自動マウントしない |

danger は warning の 3 倍の重みでスコアに効きます。Job / CronJob にはプローブのチェックを適用しません。

### 除外と重大度の変更

ワークロード単位では、アノテーションで除外できます（以前の `kube-readiness/ignore` も引き続き使えます）。

```yaml
metadata:
  annotations:
    kube-postcheck/ignore: SEC-005,REL-003
```

まとめて設定する場合は `--config` に YAML を渡します。

```yaml
checks:
  SEC-005: ignore      # 読み取り専用ルートFSはチームでは採用しない
  REL-006: danger      # 分散配置は必須にする
exemptions:
  - namespaces: ["monitoring"]
    kinds: [DaemonSet]
    checks: [SEC-002, SEC-008]   # node-exporter はホストのメトリクスを読む
```

| フラグ | 既定値 | 説明 |
|---|---|---|
| `--cluster` | `false` | ファイルではなく現在の kubeconfig のクラスタを読む |
| `--context` / `--kubeconfig` / `--namespace` | - | クラスタモードの接続先と対象 |
| `--exclude-namespace` | `kube-system,kube-public,kube-node-lease` | 対象外の namespace |
| `--config` | - | 重大度の変更と除外の設定 |
| `--output` | `table` | `table` / `json` / `markdown` / `sarif` |
| `--min-score` | `0` | 全体スコアがこれ未満なら終了コード 1 |
| `--fail-on-danger` | `false` | danger があれば終了コード 1 |
| `--timeout` | `60s` | クラスタモードの API 呼び出し全体のタイムアウト |
| `--list` | - | 採点のチェック一覧を表示 |

終了コードは通常のチェックと同じく、`0` 問題なし / `1` しきい値を下回った・danger がある・クラスタを読めなかった / `2` 引数や設定の誤り です。

## 必要な権限

すべて読み取り（list）のみです。cluster-admin を使わずに済むよう、最小権限の ServiceAccount を [`deploy/rbac.yaml`](deploy/rbac.yaml) に用意しています。
`certificates` チェックのために Secret の list 権限が必要です。付与したくない場合はその行を削除し、`--skip certificates` で実行してください。
Job・CronJob・PodDisruptionBudget の list 権限は `score --cluster` のためのものです。

## CI での利用例

```yaml
- name: Post-build check
  run: kube-postcheck --output markdown --fail-on warn | tee -a "$GITHUB_STEP_SUMMARY"
```

このリポジトリ自身の CI でも、[kind](https://kind.sigs.k8s.io/) で作ったクラスタに対して実行しています（`.github/workflows/ci.yml` の `e2e` ジョブ）。
`e2e-score` ジョブでは kind クラスタに [`examples/`](examples) をデプロイし、`score --cluster` で良い例が 100 点、悪い例で各 danger が検出されることを確かめています（[`e2e/score.sh`](e2e/score.sh)）。

```yaml
- name: Score manifests
  run: kube-postcheck score --min-score 80 --fail-on-danger --output markdown k8s/ | tee -a "$GITHUB_STEP_SUMMARY"
```

## 開発

```sh
make test   # go test -race
make lint   # golangci-lint
make cover  # カバレッジ
```

各チェックは `client-go` の fake clientset を使ったテーブルテストで検証しているため、実クラスタなしでテストできます。
新しいチェックは `check.Checker` インターフェースを実装し、`internal/check/registry.go` に追加します。
採点のチェックは `internal/readiness` にあり、良い例・悪い例のマニフェスト（[`examples/`](examples)）で検証しています（PDB の割合はディスラプションコントローラと同じく切り上げで計算）。

## License

MIT
