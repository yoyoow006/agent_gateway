#!/usr/bin/env bash
# 内部验证 core：仅供公共 wrapper 与契约测试调用，不构成 Verify/Archive 证据。
set -u
cd "$(dirname "$0")/../.."
export PYTHONDONTWRITEBYTECODE=1

pass_count=0
fail_count=0
skip_count=0
require_openspec=0
required_codex=1
required_claude=1
profile_expected=absent
external_git_root=""

cleanup_external_git() {
  if test -n "$external_git_root"; then
    rm -rf -- "$external_git_root"
  fi
}
trap cleanup_external_git EXIT

report_pass() {
  printf '[PASS] %s\n' "$1"
  pass_count=$((pass_count + 1))
}

report_fail() {
  printf '[FAIL] %s\n' "$1"
  fail_count=$((fail_count + 1))
}

report_skip() {
  printf '[SKIP] %s\n' "$1"
  skip_count=$((skip_count + 1))
}

# 唯一权威的外部命令清单：校验沙箱 PATH 白名单从此处解析，
# 不得在其他文件维护第二份副本。新增依赖时同步本清单（保持排序）。
print_external_commands() {
  printf '%s\n' \
    awk \
    bash \
    cat \
    chmod \
    cmp \
    cp \
    dirname \
    find \
    git \
    grep \
    head \
    mktemp \
    rm \
    rmdir \
    sed \
    sort \
    stat \
    tail \
    touch \
    tr \
    wc
}

read_profile_token() {
  python3 -B -c "
import hashlib
import json
import os
import stat
import sys

def reject_duplicates(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate key')
        result[key] = value
    return result

def version(metadata):
    return (
        metadata.st_dev,
        metadata.st_ino,
        stat.S_IFMT(metadata.st_mode),
        metadata.st_size,
        metadata.st_mtime_ns,
        metadata.st_ctime_ns,
    )

try:
    descriptor = os.open(
        sys.argv[1],
        os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0) | getattr(os, 'O_CLOEXEC', 0),
    )
    try:
        before = os.fstat(descriptor)
        if not stat.S_ISREG(before.st_mode):
            raise ValueError('not regular')
        chunks = []
        while True:
            chunk = os.read(descriptor, 1024 * 1024)
            if not chunk:
                break
            chunks.append(chunk)
        after = os.fstat(descriptor)
    finally:
        os.close(descriptor)
    if version(before) != version(after):
        raise ValueError('changed during read')
    content = b''.join(chunks)
    profile = json.loads(
        content.decode('utf-8', errors='strict'),
        object_pairs_hook=reject_duplicates,
    )
    if type(profile) is not dict or set(profile) != {'schema_version', 'assistant'}:
        raise ValueError('invalid keys')
    if type(profile['schema_version']) is not int or profile['schema_version'] != 1:
        raise ValueError('invalid schema')
    if type(profile['assistant']) is not str or profile['assistant'] not in {'codex', 'claude'}:
        raise ValueError('invalid assistant')
except (OSError, UnicodeError, json.JSONDecodeError, ValueError, TypeError):
    raise SystemExit(1)

fields = (
    profile['assistant'],
    *(str(value) for value in version(after)),
    hashlib.sha256(content).hexdigest(),
)
sys.stdout.write('|'.join(fields))
" "$1" 2>/dev/null
}

load_required_assistants() {
  local profile=.ai/assistant-profile.json
  local assistant token

  required_codex=1
  required_claude=1
  profile_expected=absent
  test -e "$profile" || test -L "$profile" || return 0
  if test -L "$profile" || ! test -f "$profile"; then
    report_fail "助手 profile 必须是普通文件"
    return 1
  fi

  if ! token="$(read_profile_token "$profile")"; then
    report_fail "助手 profile 格式非法"
    return 1
  fi
  assistant=${token%%|*}

  case "$assistant" in
    codex)
      required_claude=0
      ;;
    claude)
      required_codex=0
      ;;
    *)
      report_fail "助手 profile 格式非法"
      return 1
      ;;
  esac
  profile_expected=$token
  report_pass "助手 profile 合法: $assistant"
}

revalidate_assistant_profile() {
  local profile=.ai/assistant-profile.json
  local current
  if test "$profile_expected" = absent; then
    ! test -e "$profile" && ! test -L "$profile"
    return
  fi
  test ! -L "$profile" && test -f "$profile" || return 1
  current="$(read_profile_token "$profile")" || return 1
  test "$current" = "$profile_expected"
}

assistant_required() {
  case "$1" in
    codex) test "$required_codex" -eq 1 ;;
    claude) test "$required_claude" -eq 1 ;;
    *) return 1 ;;
  esac
}

internal_result() {
  printf 'INTERNAL_RESULT PASS=%d FAIL=%d SKIP=%d\n' "$pass_count" "$fail_count" "$skip_count"
}

while test "$#" -gt 0; do
  case "$1" in
    --require-openspec)
      require_openspec=1
      ;;
    --print-external-commands)
      print_external_commands
      exit 0
      ;;
    *)
      report_fail "参数错误: 未知参数 $1"
      internal_result
      exit 2
      ;;
  esac
  shift
done

if ! load_required_assistants; then
  internal_result
  exit 1
fi

check() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    report_pass "$label"
  else
    report_fail "$label"
  fi
}

check_required_test() {
  local label="$1"
  local output
  shift
  output="$(mktemp)"
  if "$@" >"$output" 2>&1; then
    report_pass "$label"
  else
    report_fail "$label"
    # 必需测试失败时保留具体用例/回溯，避免只有聚合标签的假诊断。
    sed 's/^/  /' "$output"
  fi
  rm -f "$output"
}

python_cache_paths_ignored() {
  local probe
  for probe in \
    scripts/tests/__pycache__/probe.cpython-314.pyc \
    .ai/tools/tests/probe.pyc \
    .ai/tools/tests/probe.pyo; do
    workflow_path_ignored "$probe" || return 1
  done
}

workflow_path_ignored() {
  local probe=$1
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git check-ignore -q -- "$probe"
    return
  fi
  if test -z "$external_git_root"; then
    external_git_root="$(mktemp -d)" || return 1
    git init -q "$external_git_root" || return 1
  fi
  git --git-dir="$external_git_root/.git" --work-tree="$PWD" \
    check-ignore -q -- "$probe"
}

contains_all() {
  local file="$1"
  shift
  local term
  for term in "$@"; do
    grep -Fq -e "$term" -- "$file" || return 1
  done
}

mode_table_ok() {
  local file="$1"
  grep -Eq '^\| 快速(模式)? \|' "$file" &&
    grep -Eq '^\| 标准(模式)? \|' "$file" &&
    grep -Eq '^\| 严格(模式)? \|' "$file"
}

proposal_ok() {
  local file="$1"
  local mode status
  mode="$(sed -n 's/^模式:[[:space:]]*//p' "$file" | head -1)"
  status="$(sed -n 's/^状态:[[:space:]]*//p' "$file" | head -1)"
  case "$mode:$status" in
    标准:待确认计划|标准:构建中|标准:待验证|标准:待归档|标准:已归档|标准:已取消) return 0 ;;
    严格:草稿|严格:待确认规范|严格:设计中|严格:待确认计划|严格:构建中|严格:待验证|严格:待归档|严格:已归档|严格:已取消) return 0 ;;
    *) return 1 ;;
  esac
}

proposal_mutation_rejected() {
  local mode="$1"
  local status="$2"
  local baseline fixture actual_mode actual_status

  baseline="$(mktemp)" || return 1
  fixture="$(mktemp)" || {
    rm -f "$baseline"
    return 1
  }
  if ! printf '%s\n' '模式: 严格' '状态: 构建中' >"$baseline"; then
    rm -f "$baseline" "$fixture"
    return 1
  fi
  if ! proposal_ok "$baseline"; then
    rm -f "$baseline" "$fixture"
    return 1
  fi
  if ! sed -e "s/^模式:.*/模式: $mode/" -e "s/^状态:.*/状态: $status/" "$baseline" >"$fixture"; then
    rm -f "$baseline" "$fixture"
    return 1
  fi
  if ! actual_mode="$(sed -n 's/^模式:[[:space:]]*//p' "$fixture")" ||
      ! actual_status="$(sed -n 's/^状态:[[:space:]]*//p' "$fixture")"; then
    rm -f "$baseline" "$fixture"
    return 1
  fi
  if test "$actual_mode" != "$mode" || test "$actual_status" != "$status"; then
    rm -f "$baseline" "$fixture"
    return 1
  fi
  if proposal_ok "$fixture"; then
    rm -f "$baseline" "$fixture"
    return 1
  fi
  rm -f "$baseline" "$fixture"
  return 0
}

policy_ok() {
  local file
  for file in "$@"; do
    grep -Eq '所有(文档|Markdown|文本)变更.*(四件套|OpenSpec)' "$file" && return 1
    grep -Eq '标准模式.*(必须|一律|强制).*(Design|独立.{0,6}计划|openspec/plan)' "$file" && return 1
    grep -Eq '(所有任务|标准模式).*(必须|一律|强制).*(双阶段|两阶段).{0,8}审查' "$file" && return 1
    grep -Eq '快速模式.*(必须|一律|强制).*(freeze manifest|manifest)' "$file" && return 1
    grep -Eq '标准模式.*(必须|一律|强制).*第二次完整(综合)?审查' "$file" && return 1
    grep -Eq '所有任务.*(必须|一律|强制).*(调用|使用).*(角色)?代理' "$file" && return 1
    grep -Eq '快速模式.*(必须|一律|强制).*(提交|合并)' "$file" && return 1
    grep -Eq '(归档|Archive).*(跳过|无需|不必).*(validate|校验|验证)' "$file" && return 1
    grep -Eq '标准模式.*(无需|不必).{0,4}(用户)?确认' "$file" && return 1
  done
  return 0
}

mutation_rejected() {
  local target="$1"
  local sentence="$2"
  local fixture
  fixture="$(mktemp)"
  cp "$target" "$fixture"
  printf '\n%s\n' "$sentence" >>"$fixture"
  if policy_ok "$fixture"; then
    rm -f "$fixture"
    return 1
  fi
  rm -f "$fixture"
}

legacy_ai_kb_body_absent() {
  local root found
  for root in \
    .codex/ai-kb/kb .codex/ai-kb/rules .codex/ai-kb/memory \
    .claude/ai-kb/kb .claude/ai-kb/rules .claude/ai-kb/memory; do
    test -d "$root" || continue
    found="$(find "$root" -type f -print -quit)"
    test -z "$found" || return 1
  done
}

active_legacy_reference_absent() {
  local result
  local targets=(.ai openspec/project.md)
  if assistant_required codex; then
    targets+=(AGENTS.md .codex/README.md .codex/skills .codex/agents)
  fi
  if assistant_required claude; then
    targets+=(CLAUDE.md .claude/skills .claude/agents)
  fi
  grep -R -E "\.(codex|claude)/ai-kb/(kb|rules|memory)" "${targets[@]}" >/dev/null 2>&1
  result=$?
  test "$result" -eq 1
}

no_mechanical_commit_rule() {
  grep -Eq '每任务至少一次|每个任务至少一次' "$@"
  case $? in
    0) return 1 ;;
    1) return 0 ;;
    *) return 1 ;;
  esac
}

frontmatter_ok() {
  local file="$1"
  local expected="$2"
  local actual
  actual="$(awk 'NR>1&&/^---/{exit} NR>1' "$file" | sed -n 's/^name:[[:space:]]*//p' | head -1 | tr -d '"[:space:]')"
  head -1 "$file" | grep -q '^---' &&
    awk 'NR>1&&/^---/{exit} NR>1' "$file" | grep -q '^description:' &&
    test "$actual" = "$expected"
}

# 镜像豁免登记守卫：受豁免前缀（^> **Codex 执行环境）的适配注记行
# 全仓合计数必须等于登记值。新增同前缀语义行会使计数超出并 FAIL，
# 强制维护者显式登记后再放行。
ADAPTER_NOTE_REGISTERED_COUNT=1

adapter_note_registry_ok() {
  local total
  total="$(grep -rh '^> \*\*Codex 执行环境' .codex/skills .claude/skills 2>/dev/null | wc -l)"
  test "$total" -eq "$ADAPTER_NOTE_REGISTERED_COUNT"
}

mirror_equal() {
  local skill="$1"
  local left right result
  left="$(mktemp)"
  right="$(mktemp)"
  # 声明的助手适配豁免：路径前缀改写、`Codex/Claude 使用` 措辞，以及
  # `> **Codex 执行环境` 适配注记行（cat -s 同时吞掉注记行留下的空行差）。
  # 新增适配差异必须在此显式登记，否则镜像检查失败。
  sed -e 's/Codex 使用 `\.codex/Agent 使用 `.__agent/g' \
      -e 's/\.codex/.__agent/g' \
      -e '/^> \*\*Codex 执行环境/d' ".codex/skills/$skill/SKILL.md" | cat -s >"$left"
  sed -e 's/Claude 使用 `\.claude/Agent 使用 `.__agent/g' \
      -e 's/\.claude/.__agent/g' \
      -e '/^> \*\*Codex 执行环境/d' ".claude/skills/$skill/SKILL.md" | cat -s >"$right"
  cmp -s "$left" "$right"
  result=$?
  rm -f "$left" "$right"
  return "$result"
}

# 已废弃工具名零残留：登记数守卫只管注记有几条，本检查管内容——
# 技能树、Codex 工具映射权威 README 与安装器资产树（存在时）的文档正文
# （*.md/*.toml）出现任一废弃 token 即视为对现行映射的漂移。
# 限定文档后缀同时避免命中校验器自身副本里的 token 清单字面量（自引用）。
retired_tool_names_absent() {
  local candidates=(.codex/skills .claude/skills .codex/README.md scripts/ai-workflow-assets)
  local targets=() path token
  for path in "${candidates[@]}"; do
    test -e "$path" && targets+=("$path")
  done
  test "${#targets[@]}" -gt 0 || return 0
  for token in 'multi_agent_v1__spawn_agent' 'send_input' 'close_agent' 'fork_context'; do
    grep -R -F -q --include='*.md' --include='*.toml' \
      -e "$token" -- "${targets[@]}" && return 1
  done
  return 0
}

# Codex 侧 parallel-agents 技能必须含现行派发工具名，与 .codex/README.md 映射一致。
adapter_note_tools_ok() {
  contains_all .codex/skills/parallel-agents/SKILL.md \
    'spawn_agent' 'followup_task' 'send_message' 'wait_agent' 'fork_turns'
}

# 受守护技能（writing-skills/parallel-agents/systematic-debugging）双树正文
# 不得残留对未随仓库迁移源文档的叙事性注记。只检查显式 SKILL.md 清单——
# 主规格与 memory 合法含该字样，不得递归扫描误伤。
skill_migration_notes_absent() {
  local agent skill
  for agent in "${required_agents[@]}"; do
    for skill in writing-skills parallel-agents systematic-debugging; do
      if grep -Fq -e "未随本仓库迁移" -- "$agent/skills/$skill/SKILL.md" 2>/dev/null; then
        return 1
      fi
    done
  done
  return 0
}

# 归档索引与目录严格 1:1：缺失、悬空、重复都视为归档数据不完整。
# 空归档且无 README（安装目标空白基线）为 vacuous 通过。
archive_index_ok() {
  local readme=openspec/archive/README.md
  local dirs entries
  dirs="$(mktemp)" || return 1
  entries="$(mktemp)" || { rm -f "$dirs"; return 1; }
  find openspec/archive -mindepth 1 -maxdepth 1 -type d -printf '%f\n' 2>/dev/null | sort >"$dirs"
  if test -s "$dirs" && ! test -f "$readme"; then
    rm -f "$dirs" "$entries"
    return 1
  fi
  if test -f "$readme"; then
    sed -n 's/^- `\([^`]*\)`.*/\1/p' "$readme" | sort >"$entries"
    if awk 'seen[$0]++ { found = 1 } END { exit !found }' "$entries"; then
      rm -f "$dirs" "$entries"
      return 1
    fi
    cmp -s "$dirs" "$entries" || { rm -f "$dirs" "$entries"; return 1; }
  fi
  rm -f "$dirs" "$entries"
}

required_agents=()
required_docs=()
assistant_required claude && required_agents+=(.claude) && required_docs+=(CLAUDE.md)
assistant_required codex && required_agents+=(.codex) && required_docs+=(AGENTS.md)

for dir in openspec/changes openspec/plan openspec/specs openspec/archive \
           .ai/kb .ai/kb/projects .ai/rules .ai/memory .ai/prompts/agents \
           .ai/tools .ai/tools/tests; do
  check "目录存在: $dir" test -d "$dir"
done
for agent in "${required_agents[@]}"; do
  check "目录存在: $agent/skills" test -d "$agent/skills"
done
if assistant_required codex; then
  check "目录存在: .codex/sdd" test -d .codex/sdd
fi

for file in openspec/AGENTS.md openspec/project.md \
            .ai/README.md .ai/rules/index.md .ai/rules/review.md \
            .ai/kb/projects/registry.json .ai/tools/project_facts.py \
            .ai/tools/review_manifest.py .ai/tools/README.md \
            scripts/workflow-pressure-scenarios.md; do
  check "文件存在: $file" test -f "$file"
done
if assistant_required codex; then
  check "文件存在: .codex/ai-kb/README.md" test -f .codex/ai-kb/README.md
  check "文件存在: .codex/README.md" test -f .codex/README.md
fi
if assistant_required claude; then
  check "文件存在: .claude/ai-kb/README.md" test -f .claude/ai-kb/README.md
fi

check "旧 ai-kb 不含平行正文" legacy_ai_kb_body_absent
check "活跃入口不引用旧 ai-kb 正文" active_legacy_reference_absent
if assistant_required codex; then
  check "Codex 旧知识入口只指向共享 .ai" contains_all .codex/ai-kb/README.md ".ai/" "禁止"
fi
if assistant_required claude; then
  check "Claude 旧知识入口只指向共享 .ai" contains_all .claude/ai-kb/README.md ".ai/" "禁止"
fi
check "项目 registry JSON 可解析" python3 -B -m json.tool .ai/kb/projects/registry.json
check "Python 缓存路径已忽略" python_cache_paths_ignored

skills=(open design build verify archive tdd subagent-driven code-review systematic-debugging verification git-worktrees parallel-agents writing-skills)
for agent in "${required_agents[@]}"; do
  for skill in "${skills[@]}"; do
    file="$agent/skills/$skill/SKILL.md"
    check "技能存在: $agent/$skill" test -f "$file"
    check "frontmatter: $agent/$skill" frontmatter_ok "$file" "$skill"
  done
done

cancel_must_be_archived() {
  local file="$1"
  local status
  status="$(sed -n 's/^状态:[[:space:]]*//p' "$file" | head -1)"
  test "$status" != "已取消"
}

for change in openspec/changes/*/; do
  test -f "${change}proposal.md" || continue
  check "proposal 模式/状态合法: ${change%/}" proposal_ok "${change}proposal.md"
  check "取消变更须在 archive: ${change%/}" cancel_must_be_archived "${change}proposal.md"
done
check "mutation: 拒绝非法状态" proposal_mutation_rejected 严格 完全非法
check "mutation: 标准拒绝严格专用草稿态" proposal_mutation_rejected 标准 草稿

policy_files=("${required_docs[@]}")
for agent in "${required_agents[@]}"; do
  policy_files+=("$agent/skills/open/SKILL.md" "$agent/skills/design/SKILL.md" "$agent/skills/build/SKILL.md" "$agent/skills/verify/SKILL.md" "$agent/skills/archive/SKILL.md")
done
for doc in "${required_docs[@]}"; do
  check "$doc 三级模式定义" mode_table_ok "$doc"
  check "$doc 标准单确认" contains_all "$doc" '一次确认' '待确认计划' '不创建'
  check "$doc 严格 9 态" contains_all "$doc" '草稿' '待确认规范' '设计中' '构建中' '待验证' '待归档' '已归档' '已取消'
  check "$doc 共享底线" contains_all "$doc" '用户' '外部' '破坏性' '验证'
  check "$doc 技能仲裁" contains_all "$doc" '仓库技能为准' '插件技能'
done
check "核心规则无统一重流程回归" policy_ok "${policy_files[@]}"

for agent in "${required_agents[@]}"; do
  check "$agent Open 快速豁免" contains_all "$agent/skills/open/SKILL.md" '快速模式' '不创建 proposal' '不切 feature' '默认不提交'
  check "$agent Open 标准直接待确认" contains_all "$agent/skills/open/SKILL.md" '`状态: 待确认计划`' '状态直接置为`待确认计划`' '唯一一次实施前确认' '不得另建 `openspec/plan`'
  check "$agent Open 需求理解" contains_all "$agent/skills/open/SKILL.md" \
    '权威事实优先' '只问决策' '当前已解锁问题分轮' '推荐答案' '等待用户回答' 'canonical term' '四件套' \
    '四个字段缺一不可' '证据: <来源：路径或用户输入>' '具体默认或条件式推荐' '条件式' '重新分类' '严格条件不得因' \
    '请求清晰且权威事实足够' '不追加仪式化访谈'
  check "$agent Design 仅严格" contains_all "$agent/skills/design/SKILL.md" 'Design 只属于严格模式' '不得创建 `openspec/plan`' '第二次实施前确认'
  check "$agent Design worktree 顺序" contains_all "$agent/skills/design/SKILL.md" '只暂存本变更四件套' '先切回记录的基线分支' '不得在 feature 已检出时执行 `git worktree add`'
  check "$agent Build 标准 feature 落点" contains_all "$agent/skills/build/SKILL.md" '创建并切到 `feature/<变更名>`' '不得在 main/master 写实现' '先切回基线'
  check "$agent Build 风险分支" contains_all "$agent/skills/build/SKILL.md" '标准小型、边界清晰任务' '严格' '可独立回滚的职责单元'
  check "$agent Verify 标准单审严格双审" contains_all "$agent/skills/verify/SKILL.md" '一次综合审查' '双阶段独立审查' '规格符合性' '代码质量'
  check "$agent Verify 严格 required 门禁" contains_all "$agent/skills/verify/SKILL.md" 'bash scripts/validate-workflow.sh --require-openspec' '不得 SKIP'
  check "$agent Verify 标准分层" contains_all "$agent/skills/verify/SKILL.md" '--fast' '改跑全量默认门禁'
  check "$agent Archive 模式分支" contains_all "$agent/skills/archive/SKILL.md" '标准模式' '严格模式' 'ADDED' '知识沉淀'
  check "$agent Archive required 门禁" contains_all "$agent/skills/archive/SKILL.md" 'bash scripts/validate-workflow.sh --require-openspec' '不得 SKIP'
  check "$agent Archive 归档索引" contains_all "$agent/skills/archive/SKILL.md" 'openspec/archive/README.md'
  check "$agent TDD 非运行时边界" contains_all "$agent/skills/tdd/SKILL.md" '运行时行为' '纯文档' '内容契约'
  check "$agent review 条件触发" contains_all "$agent/skills/code-review/SKILL.md" '标准小任务' '严格模式' '综合审查'
  check "$agent review 输出契约" contains_all "$agent/skills/code-review/SKILL.md" 'Verdict: PASS | FAIL' 'file:line' 'Verification:' 'Unresolved:'
  check "$agent subagent 条件触发" contains_all "$agent/skills/subagent-driven/SKILL.md" '独立任务' '标准小任务' '严格模式'
  check "$agent worktree 风险触发" contains_all "$agent/skills/git-worktrees/SKILL.md" '快速模式' '标准模式' '严格模式' '脏工作区'
done

check "归档索引与目录 1:1" archive_index_ok

check "共享 Review 规则" contains_all .ai/rules/review.md 'review_manifest.py verify' 'STALE' 'accepted-risk' '未验证范围' '残余风险'
for role in explorer reviewer test-worker; do
  check "共享角色契约: $role" test -f ".ai/prompts/agents/$role.md"
done
role_adapters=()
if assistant_required codex; then
  role_adapters+=(
    ".codex/agents/explorer.toml|.ai/prompts/agents/explorer.md"
    ".codex/agents/reviewer.toml|.ai/prompts/agents/reviewer.md"
    ".codex/agents/test_worker.toml|.ai/prompts/agents/test-worker.md"
  )
fi
if assistant_required claude; then
  role_adapters+=(
    ".claude/agents/explorer.md|.ai/prompts/agents/explorer.md"
    ".claude/agents/reviewer.md|.ai/prompts/agents/reviewer.md"
    ".claude/agents/test-worker.md|.ai/prompts/agents/test-worker.md"
  )
fi
for adapter_spec in "${role_adapters[@]}"; do
  check "角色适配: ${adapter_spec%%|*}" contains_all "${adapter_spec%%|*}" "${adapter_spec#*|}"
done

if assistant_required codex && assistant_required claude; then
  for skill in open design build verify archive tdd code-review subagent-driven git-worktrees verification parallel-agents systematic-debugging writing-skills; do
    check "双套语义镜像: $skill" mirror_equal "$skill"
  done
  check "适配注记登记数" adapter_note_registry_ok
fi

check "废弃工具名零残留" retired_tool_names_absent
if assistant_required codex; then
  check "注记现行工具名" adapter_note_tools_ok
fi
check "技能迁移注记零残留" skill_migration_notes_absent
for agent in "${required_agents[@]}"; do
  check "$agent writing-skills 字符口径锚串" contains_all "$agent/skills/writing-skills/SKILL.md" 'tr -d' 'wc -m'
  check "$agent writing-skills 场景重跑绑定" contains_all "$agent/skills/writing-skills/SKILL.md" 'workflow-pressure-scenarios.md' '重跑'
done

primary_doc="${required_docs[0]}"
check "mutation: 入口拒绝文档统一四件套" mutation_rejected "$primary_doc" "所有文档变更必须创建四件套和 OpenSpec。"
check "mutation Q: 快速拒绝强制 manifest" mutation_rejected "$primary_doc" "快速模式必须创建 freeze manifest 后才能修改文件。"
check "mutation S: 标准拒绝第二次完整审查" mutation_rejected "$primary_doc" "标准模式必须在修复后执行第二次完整综合审查。"
check "mutation X: 拒绝所有任务机械调用角色" mutation_rejected "$primary_doc" "所有任务必须调用角色代理后才能开始。"
check "mutation: 快速拒绝自动提交合并" mutation_rejected "$primary_doc" "快速模式完成后必须自动提交并合并到主分支。"
check "mutation: 标准拒绝免确认实现" mutation_rejected "$primary_doc" "标准模式四件套产出后无需用户确认即可直接实现。"
check "mutation: 归档拒绝跳过校验" mutation_rejected "$primary_doc" "归档前无需运行 validate-workflow.sh 校验，直接移动目录完成归档。"
for agent in "${required_agents[@]}"; do
  check "mutation: $agent Open 拒绝文档统一四件套" mutation_rejected "$agent/skills/open/SKILL.md" "所有文档变更必须创建四件套和 OpenSpec。"
  check "mutation: $agent Design 拒绝标准独立计划" mutation_rejected "$agent/skills/design/SKILL.md" "标准模式必须进入 Design 并创建独立计划。"
  check "mutation: $agent Build 拒绝标准双阶段审查" mutation_rejected "$agent/skills/build/SKILL.md" "标准模式必须执行双阶段审查。"
  check "mutation: $agent Verify 拒绝标准双阶段审查" mutation_rejected "$agent/skills/verify/SKILL.md" "标准模式必须执行双阶段审查。"
  check "mutation: $agent Open 拒绝快速自动提交" mutation_rejected "$agent/skills/open/SKILL.md" "快速模式完成后必须自动提交并合并到主分支。"
  check "mutation: $agent Build 拒绝标准免确认" mutation_rejected "$agent/skills/build/SKILL.md" "标准模式四件套产出后无需用户确认即可直接实现。"
  check "mutation: $agent Archive 拒绝跳过归档校验" mutation_rejected "$agent/skills/archive/SKILL.md" "归档前无需运行 validate-workflow.sh 校验，直接移动目录完成归档。"
done
check "Q/R/S/X 压力契约" contains_all scripts/workflow-pressure-scenarios.md \
  'Q：既有接口的纯文档维护' '不创建 OpenSpec' 'freeze manifest' '角色代理' \
  'R：模糊需求先建立共识' '项目 registry' 'tracked 搜索' '目标代码位置' '每题带证据' '不生成四件套' '请求本身已清晰' \
  'S：普通单模块运行时代码小改' '一次全 diff 综合 Verify' '修复后只审 delta' \
  'X：权限与数据库迁移' '任务级审查' '双阶段独立审查' '有效 manifest'
check "W/A/M 压力契约" contains_all scripts/workflow-pressure-scenarios.md \
  'W：严格实现前的 worktree 原子顺序' '已检出的分支' '只暂存本变更明确文件' \
  'A：归档合并与用户取消' '第二真源' '取消原因' '不得自行取消' \
  'M：审查中途 manifest STALE' '不沿用旧结论' '重新 freeze'
check "I 压力契约" contains_all scripts/workflow-pressure-scenarios.md \
  'I：目标已有助手入口' 'AGENTS.pre-codex-workflow.md' 'SHA-256' \
  '不输出既有正文' '--force' '临时空目录'

if assistant_required codex; then
  check "SDD 草稿区已忽略" workflow_path_ignored .codex/sdd/validation-probe
fi
if assistant_required claude; then
  check "Claude SDD 草稿区已忽略" workflow_path_ignored .claude/sdd/validation-probe
fi
mechanical_files=("${required_docs[@]}")
for agent in "${required_agents[@]}"; do
  mechanical_files+=(
    "$agent/skills/open/SKILL.md" "$agent/skills/design/SKILL.md"
    "$agent/skills/build/SKILL.md" "$agent/skills/verify/SKILL.md"
    "$agent/skills/archive/SKILL.md" "$agent/skills/code-review/SKILL.md"
    "$agent/skills/subagent-driven/SKILL.md"
  )
done
check "无每任务机械提交规则" no_mechanical_commit_rule "${mechanical_files[@]}"
if assistant_required codex; then
  check "Codex 工具映射" contains_all .codex/README.md "spawn_agent" "fork_turns" "update_plan" "apply_patch"
fi

# 内部 core 不运行顶层 contract；公共 wrapper 必须无条件执行该套件。
check_required_test "事实工具必需测试（registry/边界/分页/ignore）" python3 -B -m unittest discover -v -s .ai/tools/tests -p test_project_facts.py
check_required_test "Review manifest 必需测试（freeze/STALE/delta）" python3 -B -m unittest discover -v -s .ai/tools/tests -p test_review_manifest.py

if command -v openspec >/dev/null 2>&1; then
  check "OpenSpec validate --all --no-interactive" openspec validate --all --no-interactive
elif test "$require_openspec" -eq 1; then
  report_fail "OpenSpec CLI 缺失；required 模式不得跳过严格校验"
else
  report_skip "OpenSpec CLI 缺失；未运行 openspec validate --all --no-interactive"
fi

if ! revalidate_assistant_profile; then
  report_fail "助手 profile 在校验期间发生变化"
fi
internal_result
if test "$fail_count" -gt 0; then
  exit 1
fi
exit 0
