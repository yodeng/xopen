[![GoDoc](https://godoc.org/github.com/shenwei356/xopen?status.png)](https://godoc.org/github.com/shenwei356/xopen)
[![Build Status](https://travis-ci.org/shenwei356/xopen.svg)](https://travis-ci.org/shenwei356/xopen)
[![Coverage Status](https://coveralls.io/repos/shenwei356/xopen/badge.svg?branch=master)](https://coveralls.io/r/shenwei356/xopen?branch=master)

# xopen

    import "github.com/shenwei356/xopen"

xopen makes it easy to get buffered (possibly `gzip`-, `xz`-, or `zstd`- compressed) readers and writers. and
close all of the associated files. `Ropen` opens a file for reading. `Wopen` opens a
file for writing. 

> This packages is forked from https://github.com/brentp/xopen ,
> but I have modified too much :(

## Usage

Here's how to get a buffered reader:

```go
// gzipped
rdr, err := xopen.Ropen("some.gz")
// xz compressed
rdr, err := xopen.Ropen("some.xz")
// zstd compressed
rdr, err := xopen.Ropen("some.zst")
// normal
rdr, err := xopen.Ropen("some.txt")
// stdin (possibly gzip-, xz-, or zstd-compressed)
rdr, err := xopen.Ropen("-")
// https://
rdr, err := xopen.Ropen("http://example.com/some-file.txt")
// Cmd
rdr, err := xopen.Ropen("| ls -lh somefile.gz")
// User directory:
rdr, err := xopen.Ropen("~/shenwei356/somefile")

checkError(err)
defer checkError(rdr.Close())
```

Writter

```go
wtr, err := xopen.Wopen("some.gz")
defer checkError(wtr.Close())

outfh.Flush()
```