. "$(dirname "$0")/lib.sh"
need "the page uses the media-grid partial" 'media-grid' internal
need "bytes go to the object store through blobs" 'blobs\.' internal
refuse "no hand-made grid layout" 'grid-template-columns' internal
exit $status
