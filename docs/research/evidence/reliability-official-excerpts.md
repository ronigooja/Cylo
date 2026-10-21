# 可靠性研究：官方摘录与获取指纹

访问日期：2026-10-21。HTML经标准库HTMLParser去除script/style并压缩空白；以下为短摘录，完整HTML仅在临时取证目录，不vendor进仓库。hash是下载字节的SHA-256，可用同URL再次下载比对，但动态页面变更不意味着本次读取无效。设计与实验边界见05正文。

## sqlite-wal

https://www.sqlite.org/wal.html

SHA-256: `f3467b530b883d4a00574fe1a898b3d121ed72764ae28cf66941080ac0badb9e`

> All processes using a database must be on the same host computer; WAL does not work over a network filesystem.

> there can only be one writer at a time.

## sqlite-pragma

https://www.sqlite.org/pragma.html#pragma_synchronous

SHA-256: `af75d7d7ab00c29e6eea98618cb1259a3919c308af60a6173215bf9798706034`

> A transaction committed in WAL mode with synchronous=NORMAL might roll back following a power loss or system crash.

> With synchronous=FULL in WAL mode, an additional sync operation of the WAL file happens after each transaction commit.

> Query or change the fullfsync flag. This flag determines whether or not the F_FULLFSYNC syncing method is used on systems that support it.

## sqlite-backup

https://www.sqlite.org/backup.html

SHA-256: `306b6cca5c7c2f93b648040ae139f14d7d3221c0d16d79b5749e69d808382e04`

> The Online Backup API was created to address these concerns.

> Because the backup process is restarted as a result of writes to the source database mid-backup, the user can be sure that when the backup operation is completed the backup database contains a consistent and up-to-date snapshot of the original.

## postgres-select

https://www.postgresql.org/docs/current/sql-select.html

SHA-256: `9aa2ffff600acc6ba9729fc068e47ae9de7cfcaf35ea82c90eaa93389b66db8e`

> Skipping locked rows provides an inconsistent view of the data, so this is not suitable for general purpose work, but can be used to avoid lock contention with multiple consumers accessing a queue-like table.

## postgres-backup

https://www.postgresql.org/docs/current/backup.html

SHA-256: `34f808ad4bce972b60f425c2dcc987ca9f050be799d7f0215c00d087ec582fa3`

> There are three fundamentally different approaches to backing up PostgreSQL data: SQL dump File system level backup Continuous archiving Each has its own strengths and weaknesses; each is discussed in turn in the following sections.

## postgres-durability

https://www.postgresql.org/docs/current/wal-reliability.html

SHA-256: `cf39c64d11a6dccbcac21d69869527b8e2efbee5afcc48d71d7a2677888775e8`

> Because disk drives are dramatically slower than main memory and CPUs, several layers of caching exist between the computer's main memory and the disk platters.

## temporal-execution

https://docs.temporal.io/workflow-execution

SHA-256: `8acc3ff45680ecba521e547f0ed20d02b11c4809e48add91dc425c309c73f51a`

> Replays are necessary and often happen to give the effect that Workflow Executions are resumable, reliable, and durable.

## temporal-activities

https://docs.temporal.io/activity-execution

SHA-256: `d97795b1991e4fc69c361f4bb65fdc304171d12b9f1cd2bd444d906a39b16633`

> Temporal doesn't detect task loss directly.

## temporal-versioning

https://docs.temporal.io/develop/safe-deployments

SHA-256: `5228a43a65b3b797a92792ef4f85ec1bad8e51e254e552945e1805c07c6a5603`

> Changes to your Workflow code that qualify as non-deterministic need to be protected by either using Worker Versioning to pin your Workflows to specific code revisions, or by using the patching APIs within your Workflow code.

## docker-security

https://docs.docker.com/engine/security/

SHA-256: `0e43afb30391e537e8497fdccd3d2c4673b2c4cde2baeca5213909f2d605c2e9`

> only trusted users should be allowed to control your Docker daemon.

## docker-network

https://docs.docker.com/engine/network/drivers/none/

SHA-256: `11c282303d8a74d0a8aa3bbaffb199fc93fe62fbd40cd7e07fd58694fedeb661`

> Within the container, only the loopback device is created.

## systemd-exec

https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html

SHA-256: `a1710bc7b196f3fbdb072661791503a4117f6d980036d9b6063ac11e1f952fe1`

> ProtectSystem=strict and ProtectHome=read-only are implied, thus prohibiting the service to write to arbitrary file system locations.

## systemd-kill

https://www.freedesktop.org/software/systemd/man/latest/systemd.kill.html

SHA-256: `b0a421f46b066cee5733515ce2fa089c02168957146464ad8202050e2c355f84`

> If set to control-group , all remaining processes in the control group of this unit will be killed on unit stop (for services: after the stop command is executed, as configured with ExecStop= ).

## git-worktree

https://git-scm.com/docs/git-worktree

SHA-256: `9f8027546d4cddbbbb797489c53ae5e24191eabf486486cfcf2656a8aaa959aa`

> Unclean worktrees or ones with submodules can be removed with --force .

## stripe-idempotency

https://docs.stripe.com/api/idempotent_requests

SHA-256: `4c6fc54872372ed4161bc4678913a548bd5b6d914e8c56f2f5694d70b20bade0`

> For API v1, Stripe saves the status code and body of the first request made with a given idempotency key after endpoint execution begins.

> For API v1, you can remove keys from the system automatically after they’re at least 24 hours old.

## temporal-definition

https://docs.temporal.io/activity-definition

SHA-256: `012d235de29574b4dece5158dd6e1468a3f38ca4aa1d87bceb4cdea54513b6df`

> Temporal recommends that Activities be idempotent.

> Activities won’t record to the Event History until they return or produce an error.

## CAO固定源码静态核验

来源：02编排专岗已clone的 `/tmp/cylo-research-cao`，本报告再次只读核对；commit `4e990a9d78b154ad76c6cf94c7b51e36fce69f21`。未运行fleet。

- URL: https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/providers/codex.py
- 文件SHA-256: `a55f40780ffb795df8018e3f58ddb1795986a581ba27c1875745df5a63106afa`
- 1368: `# --yolo (alias for --dangerously-bypass-approvals-and-sandbox)`
- 1397: `"contains '*', which forces --yolo and discards it. This worker "`
- 1405: `# --yolo is --dangerously-bypass-approvals-and-sandbox, so NOTHING in`
- 1411: `"Terminal %s: launching codex with --yolo "`
- 1419: `command_parts = ["codex", "--yolo"]`

- URL: https://github.com/awslabs/cli-agent-orchestrator/blob/4e990a9d78b154ad76c6cf94c7b51e36fce69f21/src/cli_agent_orchestrator/services/worktree_service.py
- 文件SHA-256: `8d7871545c2fcd734ce30558a86a8d9127b3dace77b89d8459ab3b8c9e36a28a`
- 172: `result = _run_git(["worktree", "remove", "--force", path], cwd=repo_root)`
- 179: `result = _run_git(["branch", "-d", branch], cwd=repo_root)`
