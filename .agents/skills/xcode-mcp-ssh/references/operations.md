# Xcode MCP operations

## One-session run

Set `XCODE_SSH_HOST` and `XCODE_PROJECT_PATH` using operator-provided values. The project path is an absolute Mac path, not a path on the Linux host. Inspect `XcodeListSchemes` and `XcodeListRunDestinations` before choosing a scheme and destination. For Cylo, `CyloMobile` is the iPhone/iPad scheme; `CyloMac` is the macOS scheme.

The helper replaces the exact argument value `$workspace` with the identifier returned by an earlier successful `XcodeOpenWorkspace` in the same batch. It does not expand shell commands or arbitrary variables. For example, after setting `XCODE_SCHEME` and `XCODE_DESTINATION` from discovery:

```sh
python3 - <<'PY' | python3 .agents/skills/xcode-mcp-ssh/scripts/mcp_ssh.py
import json, os
print(json.dumps([
    ["XcodeOpenWorkspace", {"path": os.environ["XCODE_PROJECT_PATH"]}],
    ["XcodeSwitchScheme", {
        "workspaceIdentifier": "$workspace",
        "schemeName": os.environ["XCODE_SCHEME"]}],
    ["XcodeSwitchRunDestination", {
        "workspaceIdentifier": "$workspace",
        "displayTitle": os.environ["XCODE_DESTINATION"]}],
    ["RunProject", {"workspaceIdentifier": "$workspace", "attachDebugger": False}],
    ["GetConsoleOutput", {
        "workspaceIdentifier": "$workspace", "tailLimit": 30,
        "pattern": "(?i)dyld|fatal|exception|crash|terminated|abort|library not loaded"}]
]))
PY
```

Keep run and console/debugger queries in one batch when possible: a launch reference from a terminated bridge was not always accessible in the next connection. Discovery-only batches can open the project and call the list tools using `$workspace`, without running it. The helper stops on an MCP error; a tool can also return a successful RPC with a failed build/launch in its payload, which must be inspected.

## Optional persistent Codex configuration

If the user requests persistent registration, run on the **Codex host**:

```sh
codex mcp add xcode -- ssh -T -o BatchMode=yes -o ConnectTimeout=8 \
  "$XCODE_SSH_HOST" xcrun mcpbridge
codex mcp list
```

This writes local Codex configuration. The temporary Python helper does not. Registration does not inject native tools into an already running conversation; reconnect or start a fresh session as required by the client. A repository skill documents and executes a workflow; it does not itself register an MCP server. No Mac-side Codex installation is required for SSH forwarding.

## Real-device troubleshooting

| Observation | Next evidence or action |
|---|---|
| MCP initializes, but workspace calls say the agent is not approved | Open the intended project with MCP and have the user handle Xcode's agent/project access dialog. Never bypass it. |
| “Choose an app to run” appears | Inspect the active scheme's `LaunchAction` and runnable. `askForAppToLaunch="Yes"` caused this in the observed Cylo checkout. For an ordinary app target, select its built app or fix the project's scheme to run that app directly within authorized project-edit scope. Cancel an unrelated app picker. |
| Phone reports an untrusted developer | Ask the user to trust only the expected signing identity under Settings → General → VPN & Device Management. Do not change trust, accounts, signing identities, or developer settings automatically. |
| Launch succeeds, followed by a black screen or exit | Query the correct launch's filtered console and crash reports before guessing. A build/launch acknowledgment alone does not prove visible UI or continued execution. |
| `Library not loaded: @rpath/WebRTC.framework/WebRTC` | Check **both** embedded `Frameworks/WebRTC.framework/WebRTC` and the app's `LC_RPATH` with `otool -l`. In the observed checkout, embedding was already present; the iOS target lacked `@executable_path/Frameworks` in `LD_RUNPATH_SEARCH_PATHS`. Fix the applicable project configurations, rebuild, and confirm installation of that binary. Do not change system library paths. |
| Installation/launch hangs; an old PID persists | Inspect `InvokeDebuggerCommand` with `process status` and a bounded timeout. The observed old app was stopped on SIGABRT in dyld. End the authorized app's debug session with `process kill`/`StopProject` before retrying. Do not repeatedly enqueue runs or kill Xcode/system processes. |
| Debug session cannot be reached | Re-list workspaces; identifiers can change. Check the physical destination and device state. Ask for unlock/replug if needed. Do not reboot devices, reset pairing, or alter services automatically. |

Use `xcrun devicectl ... --help` for the installed Xcode's exact syntax. As a disclosed fallback when MCP cannot complete deployment, Xcode's device CLI can inspect, install, and launch the authorized app over the existing USB connection:

```sh
# Run on the Mac, using the discovered device name/identifier and built app path.
xcrun devicectl device info processes --device "$DEVICE" --search Cylo --timeout 10
xcrun devicectl device info files --device "$DEVICE" \
  --domain-type systemCrashLogs --search Cylo --timeout 10
xcrun devicectl device install app --device "$DEVICE" "$BUILT_APP" --timeout 45
xcrun devicectl device process launch --device "$DEVICE" com.cylo.mobile --timeout 15
```

Use separate bounded calls and inspect each result. After a timeout, check the active process/debugger and install outcome before retrying; it may have completed partially. Do not uninstall to recover from a deployment timeout, since that can remove app data. If no concrete recoverable cause is found, report the pending device/user action instead of continuing retries. Keep crash copies and build artifacts under the repository's approved dedicated Mac workspace, not in system directories; extract only relevant diagnostics and do not commit raw private reports.

## Verification boundary

The motivating session exercised SSH-forwarded Apple MCP on Xcode 27.1, project authorization, scheme/device discovery, and a physical iPhone 13 mini. An iOS runpath fix was made in the remote project checkout, installation later succeeded using `devicectl`, a new process was observed, and the user reported visible UI. Those remote edits are not automatically integrated into this repository. There was no complete functional, voice, APNs, or cross-device acceptance. The earlier session used an ad hoc probe. The bundled helper was subsequently checked against the real Mac using read-only tool discovery and workspace listing; local protocol fixtures checked pagination, fragmented frames, notifications, same-session workspace substitution, stop-on-error, and timeout cleanup. Those fixtures do not establish device deployment through the new helper.

Sources: [Apple: external agents and Xcode](https://developer.apple.com/documentation/xcode/giving-external-agents-access-to-xcode), [Codex MCP configuration](https://developers.openai.com/codex/mcp/), [Codex repository skills](https://developers.openai.com/codex/skills/).
