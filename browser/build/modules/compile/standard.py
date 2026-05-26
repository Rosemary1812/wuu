#!/usr/bin/env python3
"""Standard single-architecture build module for BrowserOS"""

import os
import shlex
import shutil
import tempfile
from pathlib import Path
from ...common.module import CommandModule, ValidationError
from ...common.context import Context
from ...common.utils import (
    run_command,
    log_info,
    log_success,
    log_warning,
    join_paths,
    IS_WINDOWS,
)


class CompileModule(CommandModule):
    produces = ["built_app"]
    requires = []
    description = "Build BrowserOS using autoninja"

    def validate(self, ctx: Context) -> None:
        if not ctx.chromium_src.exists():
            raise ValidationError(f"Chromium source not found: {ctx.chromium_src}")

        if not ctx.browseros_chromium_version:
            raise ValidationError("BrowserOS chromium version not set")

        args_file = ctx.get_gn_args_file()
        if not args_file.exists():
            raise ValidationError(
                f"Build not configured - args.gn not found: {args_file}"
            )

    def execute(self, ctx: Context) -> None:
        log_info("\n🔨 Building BrowserOS (this will take a while)...")

        self._create_version_file(ctx)

        ninja_targets = _configured_ninja_targets()
        if ninja_targets:
            log_info(f"Using explicit Ninja target(s): {', '.join(ninja_targets)}")
        else:
            ninja_targets = ["chrome", "chromedriver"]
            log_info("Using default Ninja target(s): chrome, chromedriver")

        ninja_cmd = _ninja_command(ctx)
        run_command(
            [ninja_cmd, "-C", ctx.out_dir, *ninja_targets], cwd=ctx.chromium_src
        )

        app_path = ctx.get_chromium_app_path()
        new_path = ctx.get_app_path()

        if app_path.exists() and not new_path.exists():
            shutil.move(str(app_path), str(new_path))

        ctx.artifact_registry.add("built_app", new_path)

        log_success("Build complete!")

    def _create_version_file(self, ctx: Context) -> None:
        parts = ctx.browseros_chromium_version.split(".")
        if len(parts) != 4:
            log_warning(f"Invalid version format: {ctx.browseros_chromium_version}")
            return

        version_content = (
            f"MAJOR={parts[0]}\n"
            f"MINOR={parts[1]}\n"
            f"BUILD={parts[2]}\n"
            f"PATCH={parts[3]}"
        )

        chrome_version_path = join_paths(ctx.chromium_src, "chrome", "VERSION")
        if (
            chrome_version_path.exists()
            and chrome_version_path.read_text() == version_content
        ):
            log_info(f"VERSION file already current: {ctx.browseros_chromium_version}")
            return

        with tempfile.NamedTemporaryFile(mode="w", delete=False) as temp_file:
            temp_file.write(version_content)
            temp_path = temp_file.name

        shutil.copy2(temp_path, chrome_version_path)
        Path(temp_path).unlink()

        log_info(f"Created VERSION file: {ctx.browseros_chromium_version}")


def build_target(ctx: Context, target: str) -> bool:
    """Build a specific target (e.g., mini_installer)"""
    log_info(f"\n🔨 Building target: {target}")

    ninja_cmd = _ninja_command(ctx)
    run_command([ninja_cmd, "-C", ctx.out_dir, target], cwd=ctx.chromium_src)

    log_success(f"Target {target} built successfully")
    return True


def _configured_ninja_targets() -> list[str]:
    targets = os.environ.get("WUU_BROWSER_NINJA_TARGETS") or os.environ.get(
        "BROWSEROS_NINJA_TARGETS", ""
    )
    if not targets.strip():
        return []
    if "," in targets:
        return [target.strip() for target in targets.split(",") if target.strip()]
    return shlex.split(targets)


def _ninja_command(ctx: Context) -> str:
    default_cmd = "autoninja.bat" if IS_WINDOWS() else "autoninja"
    path_cmd = shutil.which(default_cmd)
    if path_cmd:
        log_info("Using autoninja from PATH")
        return path_cmd

    depot_tools_cmd = ctx.chromium_src / "third_party" / "depot_tools" / default_cmd
    depot_tools_python = (
        ctx.chromium_src / "third_party" / "depot_tools" / "python3_bin_reldir.txt"
    )
    if depot_tools_cmd.exists() and depot_tools_python.exists():
        log_info(f"Using Chromium checkout autoninja: {depot_tools_cmd}")
        return str(depot_tools_cmd)

    ninja_name = "ninja.exe" if IS_WINDOWS() else "ninja"
    bundled_ninja = ctx.chromium_src / "third_party" / "ninja" / ninja_name
    if bundled_ninja.exists():
        log_warning(
            "autoninja not found or checkout depot_tools is not bootstrapped; "
            f"using Chromium checkout ninja: {bundled_ninja}"
        )
        return str(bundled_ninja)

    log_warning(
        "autoninja not found in PATH or Chromium checkout; falling back to PATH lookup"
    )
    return default_cmd
