#!/usr/bin/env bash
# 公共工作流门禁：对任何调用环境都运行内部 core 与顶层契约套件。
# --fast 仅运行 core（秒级），供标准模式 Verify 终验分层使用；全量仍是默认。
set -u
cd "$(dirname "$0")/.."
export PYTHONDONTWRITEBYTECODE=1

fast_mode=0
archive_light=0
require_openspec_user=0
archive_files=()
forwarded_arguments=()
while test "$#" -gt 0; do
  argument="$1"
  case "$argument" in
    --fast)
      fast_mode=1
      shift
      ;;
    --archive-light)
      archive_light=1
      shift
      ;;
    --archive-files)
      shift
      while test "$#" -gt 0; do
        case "$1" in
          --*)
            break
            ;;
          *)
            archive_files+=("$1")
            shift
            ;;
        esac
      done
      ;;
    --require-openspec)
      require_openspec_user=1
      forwarded_arguments+=("$argument")
      shift
      ;;
    --print-external-commands)
      # 诊断模式直接透传 core(只读、无汇总语义),不进入门禁流程。
      exec bash scripts/lib/validate-workflow-core.sh --print-external-commands
      ;;
    *)
      forwarded_arguments+=("$argument")
      shift
      ;;
  esac
done

# --archive-light 与 --require-openspec 互斥（语义冲突：前者跳过契约套件、后者强制契约套件）
if test "$archive_light" -eq 1 && test "$require_openspec_user" -eq 1; then
  printf "[FAIL] --archive-light 与 --require-openspec 冲突（conflict）：归档轻量门禁与强制完整门禁不能同时启用\n" >&2
  exit 2
fi
if test "$fast_mode" -eq 1 && test "$require_openspec_user" -eq 1; then
  printf "[FAIL] --fast 与 --require-openspec 冲突（conflict）：快速分层不能替代强制完整门禁\n" >&2
  exit 2
fi
if test "$fast_mode" -eq 1 && test "$archive_light" -eq 1; then
  printf "[FAIL] --fast 与 --archive-light 冲突（conflict）：两种轻量入口语义不能叠加\n" >&2
  exit 2
fi

# --archive-light 单独使用 = 纯轻量（仅跑 core，不做 diff 分类）
#   配合 --archive-files + WORKFLOW_ARCHIVE_GATE=1 才做 diff 分类自动升级

# Archive 的 diff 分类必须在运行 core 前完成：命中语义变化时，本次调用从一开始
# 就按 required 语义执行，而不是只追加顶层契约套件。
archive_promoted=0
if test "$archive_light" -eq 1; then
  printf "[INFO] archive-light gate engaged (files=%d)\n" "${#archive_files[@]}"
  if test "${WORKFLOW_ARCHIVE_GATE:-0}" = "1" && test "${#archive_files[@]}" -gt 0; then
    diff_base="${WORKFLOW_ARCHIVE_BASE:-HEAD}"
    if ! command -v git >/dev/null 2>&1; then
      printf "[FAIL] WORKFLOW_ARCHIVE_GATE=1 但 git 不可用；无法执行 diff 分类自动升级。请安装 git 或撤销 WORKFLOW_ARCHIVE_GATE。\n" >&2
      exit 2
    fi
    diff_output=""
    if ! diff_output="$(git diff --name-only "$diff_base" -- ${archive_files[@]+"${archive_files[@]}"} 2>&1)"; then
      printf "[FAIL] Archive diff 分类失败（base=%s）：%s\n" "$diff_base" "$diff_output" >&2
      exit 2
    fi
    if test -n "$diff_output"; then
      while IFS= read -r diff_file; do
        test -n "$diff_file" && printf "[INFO] archive gate promoted to --require-openspec: %s\n" "$diff_file"
      done <<EOF
$diff_output
EOF
      archive_promoted=1
      forwarded_arguments+=(--require-openspec)
    fi
  fi
fi

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

# 顶层契约套件只在此函数内出现一次,便于静态检查与 mutation 测试。
run_contract_suite() {
  if test "${WORKFLOW_TEST_JOBS:-0}" = "1"; then
    python3 -B -m unittest -v scripts.tests.test_validate_workflow >"$contract_output" 2>&1
  else
    python3 -B scripts/tests/run_validate_workflow_parallel.py --module scripts.tests.test_validate_workflow >"$contract_output" 2>&1
  fi
}

# 契约套件内部设计性跳过（源仓专属能力，如 CI / pre-push 钩子）必须
# 在汇总前逐条可见；不计入顶层 SKIP 字段。失败/解析错误直接 fail-closed。
render_contract_suite_skips() {
  contract_skip_status=0
  contract_skips="$(grep -c '\.\.\. skipped' "$contract_output")" || contract_skip_status=$?
  case "$contract_skip_status" in
    0|1)
      case "$contract_skips" in
        ""|*[!0-9]*)
          printf "[FAIL] 契约套件内部跳过计数解析失败（结果必须为非负整数）\n"
          fail_count=$((fail_count + 1))
          contract_skips=0
          ;;
      esac
      ;;
    *)
      printf "[FAIL] 契约套件内部跳过计数解析失败（grep exit %d）\n" "$contract_skip_status"
      fail_count=$((fail_count + 1))
      contract_skips=0
      ;;
  esac
  if test "$contract_skips" -gt 0; then
    printf "  契约套件内部设计性跳过 %d 项（源仓专属能力；不影响门禁计数）:\n" "$contract_skips"
    sed -n '/\.\.\. skipped/{s/^/  - /;p;}' "$contract_output"
    # $? 必须紧跟 sed 捕获其退出码；本行与 sed 之间不得插入任何命令。
    contract_skip_render_status=$?
    if test "$contract_skip_render_status" -ne 0; then
      printf "[FAIL] 契约套件内部跳过明细渲染失败（sed exit %d）\n" "$contract_skip_render_status"
      fail_count=$((fail_count + 1))
    fi
  fi
}

report_contract_suite_success() {
  contract_count="$(sed -n 's/^Ran \([1-9][0-9]*\) tests\{0,1\}\( in .*\)\{0,1\}$/\1/p' "$contract_output" | tail -1)"
  if test -n "$contract_count"; then
    printf "[PASS] 工作流顶层契约测试（%s tests）\n" "$contract_count"
    return 0
  fi
  printf "[FAIL] 契约套件用例计数解析失败\n"
  sed "s/^/  /" "$contract_output"
  return 1
}

core_status=0
if test "$fast_mode" -eq 1; then
  WORKFLOW_FAST_CACHE=1 bash scripts/lib/validate-workflow-core.sh ${forwarded_arguments[@]+"${forwarded_arguments[@]}"} >"$core_output" 2>&1 || core_status=$?
else
  # 非 fast 门禁必须实际执行：显式关闭外部可能继承的缓存开关。
  WORKFLOW_FAST_CACHE=0 bash scripts/lib/validate-workflow-core.sh ${forwarded_arguments[@]+"${forwarded_arguments[@]}"} >"$core_output" 2>&1 || core_status=$?
fi
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

# 归档轻量门禁：跳过顶层契约套件，core 结果已足；diff 分类自动升级如下。
# 1) WORKFLOW_ARCHIVE_GATE=1 + --archive-files 非空 + git diff --name-only <base> -- <files> 非空 → 改跑契约套件
if test "$archive_light" -eq 1; then
  if test "$archive_promoted" -eq 1; then
    # 改跑契约套件（与正常路径同一段）
    if run_contract_suite; then
      if report_contract_suite_success; then
        pass_count=$((pass_count + 1))
      else
        fail_count=$((fail_count + 1))
      fi
    else
      printf "[FAIL] 工作流顶层契约测试（promoted）\n"
      fail_count=$((fail_count + 1))
      sed "s/^/  /" "$contract_output"
    fi
    render_contract_suite_skips
  fi
  printf "PASS=%d FAIL=%d SKIP=%d\n" "$pass_count" "$fail_count" "$skip_count"
  if test "$fail_count" -gt 0 || test "$core_status" -ne 0; then
    exit 1
  fi
  exit 0
fi

if run_contract_suite; then
  if report_contract_suite_success; then
    pass_count=$((pass_count + 1))
  else
    fail_count=$((fail_count + 1))
  fi
else
  printf "[FAIL] 工作流顶层契约测试\n"
  fail_count=$((fail_count + 1))
  sed "s/^/  /" "$contract_output"
fi
render_contract_suite_skips

printf "PASS=%d FAIL=%d SKIP=%d\n" "$pass_count" "$fail_count" "$skip_count"
if test "$fail_count" -gt 0 || test "$core_status" -ne 0; then
  exit 1
fi
exit 0
