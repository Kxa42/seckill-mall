#!/usr/bin/env bash
set -euo pipefail

# 保留旧脚本名作为本地验收入口，但只执行统一阶段5 Memory/Fake 测试。
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec bash "$repo_root/tests/stage5_memory_e2e.sh"
