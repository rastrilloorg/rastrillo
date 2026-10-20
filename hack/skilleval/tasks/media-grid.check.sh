. "$(dirname "$0")/lib.sh"
need "the page uses the media-grid partial" 'template "media-grid"' internal
need "bytes go to the object store through blobs" 'blobs\.[A-Z]' internal
refuse "no hand-made grid layout" 'grid-template-columns' -- internal
exit $status
