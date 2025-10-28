#!/bin/bash

(
	opExit=exit
	opCancel=cancel

	cmds=()

	echo "onexit: running"

	while read -r cmd; do
		case "$cmd" in
		${opExit}:*)
			command="${cmd#${opExit}:}"
			echo "onexit: queued: $command"
			cmds+=("$command")
			;;
		${opCancel}:*)
			i=${cmd#${opCancel}:}
			echo "onexit: cancelling $i: ${cmds[$i]}"
			cmds[$i]=""
			;;
		*)
			echo "onexit: invalid command: $cmd"
			;;
		esac
	done

	echo "onexit: stdin closed - running commands"

	for cmd in "${cmds[@]}"; do
		if [ -n "$cmd" ]; then
			echo "onexit: > $cmd"
			eval "$cmd" || echo "onexit: command failed: $cmd"
		fi
	done
)
