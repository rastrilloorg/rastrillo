// Fails the way the two kinds of failure look: a line on stdout (where
// node --test reports) and a line on stderr (where a crash lands).
process.stdout.write("stdout-says-why\n");
process.stderr.write("stderr-says-why\n");
process.exit(3);
