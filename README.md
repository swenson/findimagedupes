# findimagedupes

`findimagedupes` finds similar and duplicate images.

This is written in Go and has no dependencies. This has a side effect
that it is limited to GIF, JPEG, and PNG files for now, but it is very easy
to install, with no ImageMagick or third-party libraries needed.

This code is a reimplementation of the algorithm used in
[`findimagedupes`](https://github.com/jhnc/findimagedupes),
a Perl program written by Jonathan H N Chin (code@jhnc.org), which is itself
a reimplementation of the algorithm used by [`findimagedupes`](https://gist.github.com/milkers/6318909) by Rob Kudla.

## Installation

`go install github.com/swenson/findimagedupes`

## Usage

`findimagedupes [flags] dir1 [dir2 ...]`

```
  -extensions string
    	file extensions to consider, comma-separated (default "jpg,jpeg,gif,png")
  -threshold float
    	percentage match for threshold (default 10)
  -verbose
    	verbose
```