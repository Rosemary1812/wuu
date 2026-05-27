#!/usr/bin/env python3
"""Build configuration module for BrowserOS build system"""

import sys
import shutil

from ...common.module import CommandModule, ValidationError
from ...common.context import Context
from ...common.utils import (
    run_command,
    log_info,
    log_warning,
    log_success,
    join_paths,
    IS_LINUX,
    IS_MACOS,
    IS_WINDOWS,
)


class ConfigureModule(CommandModule):
    produces = []
    requires = []
    description = "Configure build with GN"

    def validate(self, ctx: Context) -> None:
        if not ctx.chromium_src.exists():
            raise ValidationError(f"Chromium source not found: {ctx.chromium_src}")

        if not ctx.paths.gn_flags_file:
            raise ValidationError("GN flags file not set")

        flags_file = join_paths(ctx.root_dir, ctx.paths.gn_flags_file)
        if not flags_file.exists():
            raise ValidationError(f"GN flags file not found: {flags_file}")

    def execute(self, ctx: Context) -> None:
        log_info(f"\n⚙️  Configuring {ctx.build_type} build for {ctx.architecture}...")

        # Linux: ensure the target-arch Debian sysroot is installed before
        # `gn gen`. sysroot.gni asserts on missing sysroots, and relying on
        # `gclient sync` DEPS hooks is fragile — the hook only fires when
        # .gclient declared the right `target_cpus` *before* sync, which
        # isn't guaranteed for chromium_src checkouts that predate
        # cross-arch support. install-sysroot.py is idempotent and fast,
        # so call it unconditionally for the target arch.
        if IS_LINUX():
            self._ensure_linux_sysroot(ctx)

        out_path = join_paths(ctx.chromium_src, ctx.out_dir)
        out_path.mkdir(parents=True, exist_ok=True)

        flags_file = join_paths(ctx.root_dir, ctx.paths.gn_flags_file)
        args_file = ctx.get_gn_args_file()

        args_content = flags_file.read_text()
        args_content += f'\ntarget_cpu = "{ctx.architecture}"\n'

        args_file.write_text(args_content)

        gn_cmd = _gn_command(ctx)
        gn_args = [gn_cmd, "gen", ctx.out_dir]
        if ctx.build_type != "debug":
            gn_args.append("--fail-on-unused-args")
        run_command(gn_args, cwd=ctx.chromium_src)

        log_success("Build configured")

    def _ensure_linux_sysroot(self, ctx: Context) -> None:
        install_script = (
            ctx.chromium_src / "build" / "linux" / "sysroot_scripts" / "install-sysroot.py"
        )
        if not install_script.exists():
            log_warning(
                f"⚠️  install-sysroot.py not found at {install_script}; "
                f"skipping sysroot bootstrap. gn gen will fail if the "
                f"{ctx.architecture} sysroot is missing."
            )
            return

        # install-sysroot.py accepts our arch names directly: it translates
        # `x64`→`amd64` internally via ARCH_TRANSLATIONS, and `arm64` is a
        # valid pass-through value.
        log_info(
            f"📦 Ensuring Linux sysroot for {ctx.architecture} (idempotent)..."
        )
        run_command(
            [sys.executable, str(install_script), f"--arch={ctx.architecture}"],
            cwd=ctx.chromium_src,
        )


def _gn_command(ctx: Context) -> str:
    default_cmd = "gn.bat" if IS_WINDOWS() else "gn"
    path_cmd = shutil.which(default_cmd)
    if path_cmd:
        log_info("Using gn from PATH")
        return path_cmd

    platform_dir = "win" if IS_WINDOWS() else "mac" if IS_MACOS() else "linux64"
    executable = "gn.exe" if IS_WINDOWS() else "gn"
    bundled_gn = ctx.chromium_src / "buildtools" / platform_dir / executable
    if bundled_gn.exists():
        log_info(f"Using Chromium checkout gn: {bundled_gn}")
        return str(bundled_gn)

    log_warning("gn not found in PATH or Chromium checkout; falling back to PATH lookup")
    return default_cmd
