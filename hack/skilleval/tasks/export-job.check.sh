. "$(dirname "$0")/lib.sh"
need "the export runs as a jobs job" 'jobs\.New|\.Start\(' internal
need "the job pages are the framework's handlers" 'jobs\.NewHandlers' internal
exit $status
