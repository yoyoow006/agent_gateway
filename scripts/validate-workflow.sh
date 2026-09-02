#!/usr/bin/env bash
# 公共工作流门禁：对任何调用环境都运行内部 core 与顶层契约套件。
# --fast 仅运行 core（秒级），供标准模式 Verify 终验分层使用；全量仍是默认。
set -u
cd "$(dirname "$0")/.."
export PYTHONDONTWRITEBYTECODE=1

fast_mode=0
forwarded_arguments=()
for argument in "$@"; do
  case "$argument" in
    --fast)
      fast_mode=1
      ;;
    --print-external-commands)
      # 诊断模式直接透传 core(只读、无汇总语义),不进入门禁流程。
      exec bash scripts/lib/validate-workflow-core.sh --print-external-commands
      ;;
    *)
      forwarded_arguments+=("$argument")
      ;;
  esac
done

# 串行化同一工作树的并发校验：契约套件含 mutation,并发实例会互踩产生假失败。
if command -v flock >/dev/null 2>&1; then
  # 注意:exec 无命令时其重定向会持久作用到当前 shell,故 2>/dev/null 必须
  # 用编组限定作用域,否则锁冲突消息会被整体吞掉。
  if mkdir -p .ai-local 2>/dev/null && { exec 9>>.ai-local/.validate.lock; } 2>/dev/null; then
    if ! flock -n 9; then
      printf '[FAIL] 另一校验实例运行中，本实例退出（并发校验会互踩）\n' >&2
      exit 2
    fi
  else
    printf '锁文件不可用，降级为无锁并发保护\n' >&2
  fi
else
  printf 'flock 不可用，降级为无锁并发保护\n' >&2
fi

core_output="$(mktemp)"
contract_output="$(mktemp)"
cleanup() {
  rm -f -- "$core_output" "$contract_output"
}
trap cleanup EXIT

core_status=0
bash scripts/lib/validate-workflow-core.sh ${forwarded_arguments[@]+"${forwarded_arguments[@]}"} >"$core_output" 2>&1 || core_status=$?
internal_result="$(sed -n "s/^INTERNAL_RESULT PASS=[0-9][0-9]* FAIL=[0-9][0-9]* SKIP=[0-9][0-9]*$/&/p" "$core_output" | tail -1)"
sed "/^INTERNAL_RESULT PASS=[0-9][0-9]* FAIL=[0-9][0-9]* SKIP=[0-9][0-9]*$/d" "$core_output"

if test -n "$internal_result"; then
  counts="${internal_result#INTERNAL_RESULT }"
  pass_field="${counts%% *}"
  counts="${counts#* }"
  fail_field="${counts%% *}"
  skip_field="${counts#* }"
  pass_count="${pass_field#PASS=}"
  fail_count="${fail_field#FAIL=}"
  skip_count="${skip_field#SKIP=}"
else
  pass_count=0
  fail_count=1
  skip_count=0
  printf "[FAIL] 内部验证 core 未返回可解析结果\n"
fi

# 参数错误保持 CLI 退出码 2；有效调用永远继续运行顶层契约套件。
if test "$core_status" -eq 2; then
  printf "PASS=%d FAIL=%d SKIP=%d\n" "$pass_count" "$fail_count" "$skip_count"
  exit 2
fi

if test "$fast_mode" -eq 1; then
  printf "PASS=%d FAIL=%d SKIP=%d\n" "$pass_count" "$fail_count" "$skip_count"
  if test "$fail_count" -gt 0 || test "$core_status" -ne 0; then
    exit 1
  fi
  exit 0
fi

if python3 -B -m unittest -v scripts.tests.test_validate_workflow >"$contract_output" 2>&1; then
  printf "[PASS] 工作流顶层契约测试\n"
  pass_count=$((pass_count + 1))
  # 透明化：套件整体计 1 个门禁检查，但内部设计性跳过（如源仓专属的
  # CI/pre-push 检查）必须在汇总前逐条可见；不计入顶层 SKIP 字段。
  contract_skips="$(grep -c '\.\.\. skipped' "$contract_output")"
  if test "$contract_skips" -gt 0; then
    printf "  契约套件内部设计性跳过 %d 项（源仓专属能力；不影响门禁计数）:\n" "$contract_skips"
    grep '\.\.\. skipped' "$contract_output" | sed 's/^/  - /'
  fi
else
  printf "[FAIL] 工作流顶层契约测试\n"
  fail_count=$((fail_count + 1))
  sed "s/^/  /" "$contract_output"
fi

printf "PASS=%d FAIL=%d SKIP=%d\n" "$pass_count" "$fail_count" "$skip_count"
if test "$fail_count" -gt 0 || test "$core_status" -ne 0; then
  exit 1
fi
exit 0
