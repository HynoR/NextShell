# NextShell v2: cwd only, installed for this shell lifetime only.
__nextshell_emit_cwd() {
  local LC_ALL=C out='' ch hex i
  for ((i=1; i<=${#PWD}; i++)); do
    ch=${PWD[i]}
    case "$ch" in [a-zA-Z0-9/._~-]) out+="$ch";;
      *) printf -v hex '%%%02X' "'$ch"; out+="$hex";; esac
  done
  printf '\033]7;file://localhost%s\007' "$out"
}
typeset -ga precmd_functions
if (( ! ${precmd_functions[(I)__nextshell_emit_cwd]} )); then precmd_functions+=(__nextshell_emit_cwd); fi
__nextshell_emit_cwd
