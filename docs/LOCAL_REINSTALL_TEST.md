# TestBridge — Reinstall & attended local acceptance

Status: instructions for **last phase only**, after GitHub Actions Windows/macOS checks pass and packages have been published from `main`. Native behavior remains UNVERIFIED until actually run. The Bridge is used for any Windows development project; CAI/RAI are examples, not default restrictions.

## Download the exact main build

1. Open [TestBridge test releases](https://github.com/thebimhouseinfo-sudo/testbridge/releases), select the latest **test-<commit SHA prefix>** release. Do **not** use the old fixed-tag `test-2026-11-03` release for this new implementation.
2. Download `testbridge-windows-amd64.zip`, `testbridge-macos-arm64.zip` and `SHA256SUMS.txt` from **the same release**.
3. Verify the checksums against the downloaded ZIP files (example on macOS: `shasum -a 256 testbridge-*.zip`; on Windows: `Get-FileHash .\testbridge-windows-amd64.zip -Algorithm SHA256`). The SHA must match `SHA256SUMS.txt`. Check the release source commit is the expected merged `main` commit.
4. Both Bridge apps must be closed before replacing the binaries. Leave the Windows application under test closed if a DLL will be replaced.

## Windows reinstall

1. Extract `testbridge-windows-amd64.zip` into a temporary directory. Verify it contains `bridge.exe` and `install.ps1`.
2. In PowerShell, run from the extracted directory: `powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1`. This installs the current-user binary and Start Menu/Desktop shortcut. Review the script and Windows prompts before execution.
3. Launch **LAN Bridge** via shortcut, choose project config in Windows UI, and **manually Open** the saved Permission Config. If this is a new project, create its profile with the intended Project ID, workspace, folders, programs and Actual Grants first.
4. Permissions (Folder/Program Grant, Edit, Revoke) are managed **only on Windows**; Mac does not edit this config. The installer is designed to preserve existing App Data; back up the existing profiles before a first major reinstall.

## Mac reinstall (Apple silicon / ARM64)

1. Extract `testbridge-macos-arm64.zip`. Its content is contained under the `mac/` folder, including `LAN Bridge.app`, `bin/bridge`, and `install.sh`.
2. In Terminal, `cd` into the extracted `mac/` directory and run `sh install.sh`. The installer places the app under `~/Applications` and `bridge` in `~/.local/bin`, then opens the app. Review the script and macOS prompts first.
3. For first-time project setup, enter/select the **same Project ID** as Windows. The **Mac app** creates a local **empty blacklist file**. For an existing project, **Mac agent may call `config_load` read-only** after connection, or the user may Open it in Mac UI.
4. Blacklist is saved locally on Mac, **never synced** to Windows. It is initially empty and Locked. If editing is requested, the user must manually Unlock in Mac UI; agent may then update only the requested entries; user can Lock again.

## Acceptance on the actual two machines (not yet performed)

- Pair once. Exit/restart and verify automatic transport reconnect. A network connection alone must not mean `PROJECT_READY`.
- Open Windows Permission Config manually, then load Mac existing blacklist. Both Project IDs must match and both config files must have been successfully read to allow Read/Sync/Exec/App/MCP.
- Verify that a **loaded empty blacklist** permits all granted project MCP tools. Test missing, malformed or unloaded blacklist: operations must not work. Confirm failed config Open does not leave old project active.
- Verify Windows can grant **more/fewer/different** capabilities than requested, independently Add Folder/Program, and Edit/Revoke during an active session.
- Read logs, on-demand Sync <= 5 MB into a granted folder outside the project root, deny ungranted directories or > 5 MB, and verify neither Bridge project config is transferred.
- Run a generic (non-CAD) Windows repository script and test Windows Shell Exec with stdout/stderr/exit. Test Application Start and Stop under Windows grants.
- Set a single explicit blacklist rule only after manual Mac Unlock, then verify that matching MCP calls are denied and unrelated calls still pass. Lock again: agent edit must fail. Test Save As/Rename/Delete and persistence across restart.
- Verify two-way chat, Exit cancellation of Bridge-launched jobs, and CPU/RAM idle measurements.
- **Optional CAI test** only when user actually chooses that repository: working vs test drawings, build DLL, restart AutoCAD and invoke CAI MCP. AutoCAD OPEN/COM is NOT blacklisted unless user adds a supported explicit rule.

Collect OS versions, binaries' SHA256, project ID (no secrets), exact operations, log excerpts and PASS/FAIL evidence. Do not mark native scenarios PASS merely because the GitHub CI runner built a binary.

## Safe rollback

Keep the previously installed binaries and a backup of local App Data profiles. If the new Bridge fails to start, close it, reinstall the previous known-good package, and restore the matching local profile backup only if needed. Never copy the Mac blacklist onto Windows or vice versa.
