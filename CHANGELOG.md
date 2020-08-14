# Change Log

All notable changes to this project will be documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1] - 2020-08-14

### Changed

- The writer now respects the 10k limit set by AWS for batch size (as well as the 1MB limit).

## [0.1.0] - 2020-08-14

### Changed

- Now sends logs in batches with a default batch interval of 5 seconds.

### Added

- Added Close method to CloudWatchWriter which blocks until all the messages have been sent.
- Added SetBatchInterval method which easily allows the user to change the interval between sending batches of logs to CloudWatch.

## [0.0.1] - 2020-08-12

### Added

- Basic synchronous implementation of NewWriter and NewWriterWithClient.

