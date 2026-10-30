# Retired AO host removal

**Status: NOT EXECUTED.** This runbook applies only to an authorized operator on
the Linux Zoe host. Do not run it against a live host without an approved change
window, current backup, and explicit confirmation of the target instance.

1. Confirm that `zoe-runner.service` is the active runtime authority and that its
   environment uses `ZOE_OPENCODE_IMAGE`, `ZOE_OPENCODE_MODEL`,
   `ZOE_OPENCODE_PROVIDER_SOCKET`, and `ZOE_OPENCODE_WORKSPACE_ROOT` from
   `/etc/zoe/zoe-runner.env`. Do not continue if the runner still depends on AO
   settings.
2. List the retired unit and drop-ins, then stop and disable it:
   `systemctl status zoe-ao.service`, `systemctl stop zoe-ao.service`, and
   `systemctl disable zoe-ao.service`. Confirm it is inactive before removal.
3. Remove the retired unit file and its drop-ins, then run
   `systemctl daemon-reload`. Remove `/etc/zoe/ao.env` and the retired
   `/opt/zoe/ao` tree only after checking each path and recording the change.
4. List legacy containers and images before removal. Use Docker label filters for
   `io.zoe.ao.*`, review the complete IDs and image references, and remove only
   the approved legacy AO objects. Do not use an unfiltered prune command.
5. Re-check `systemctl is-active zoe-runner.service`, inspect the runner
   environment without printing secret values, and record the result and the
   removal receipt. This runbook does not authorize changes to the external
   CLIProxyAPI deployment or any live host by itself.
