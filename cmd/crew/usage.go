package main

const usage = `crew — compile agents and skills across AI coding harnesses

Usage:
  crew init     [--dir D]                 scaffold a crew directory
  crew verify   [--dir D] [-v]            strict check of config + sources
  crew models   [--harness H | --all]     list every model + effort each harness can run
  crew models sync                        refresh models.yaml from the catalog (models.dev)
  crew capabilities [--harness H | --all] which native tools each capability becomes
  crew plan     [--dir D] [scope flags]   show what each harness gets, models, costs, notes, file changes
  crew compile  [--dir D] [scope flags]   verify, then write files
  crew run <factory> --input TEXT|@FILE [--detach]   run a factory (see crew run --help)
  crew run status|say|goto|approve|resume <id> …    watch and steer a run
  crew factory list | diagram <name>     list factories, or print one as a Mermaid flowchart
  crew domain list [--dir D]              list domains (name, factory count, description)
  crew repo confirm|status [--project P] confirm a repo for missions (in a terminal), or show its status
  crew automerge on|off|status           set auto-merge per domain (in a terminal), or show it
  crew jira setup|check|sync             show missions on a Jira board (setup needs a terminal)
  crew mission setup|plan|start|go|status|answer|amend|stop|resume   run a mission (see crew mission)
  crew migrate  [--from ~/crewfile] [--to ~/crew] [--dry-run]
                                         move an old crewfile setup to crew's names

Scope flags:
  --scope user|project   write to ~/... (default) or into a project's dot-dirs
  --project PATH         project root for --scope project
  --out DIR              stage everything under DIR instead of the real locations

Other flags:
  --dry-run              (compile) show actions without writing
  --force                (compile) overwrite/delete files crew doesn't own or that were hand-edited
  --allow-warnings       don't fail on warnings (errors still fail)
  -v                     also print info notes

Directory resolution: --dir, then $CREW_HOME, then the current directory if it
contains crew.yaml, then ~/crew.
`
