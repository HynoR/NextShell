# NextShell v2: cwd only. No OSC 133, observers, downloads or persistent rc edits.
__nextshell_emit_cwd() {
  local LC_ALL=C out='' ch hex i
  for ((i=0; i<${#PWD}; i++)); do
    ch=${PWD:i:1}
    case "$ch" in [a-zA-Z0-9/._~-]) out+="$ch";;
      *) printf -v hex '%%%02X' "'$ch"; out+="$hex";; esac
  done
  printf '\033]7;file://localhost%s\007' "$out"
}
if [[ $(declare -p PROMPT_COMMAND 2>/dev/null || true) == 'declare -a'* ]]; then
  if [[ " ${PROMPT_COMMAND[*]} " != *' __nextshell_emit_cwd '* ]]; then PROMPT_COMMAND+=(__nextshell_emit_cwd); fi
else
  case ";${PROMPT_COMMAND:-};" in *'__nextshell_emit_cwd;'*) ;;
    *) # A newline preserves trailing semicolons AND existing end-of-line comments.
       if [[ -n ${PROMPT_COMMAND:-} ]]; then PROMPT_COMMAND+=$'\n'; fi
       PROMPT_COMMAND+='__nextshell_emit_cwd';;
  esac
fi
__nextshell_emit_cwd
