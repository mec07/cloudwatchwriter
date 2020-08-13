# Change Log

All notable changes to this project will be documented in this file. The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2020-08-14

### Changed

- Now sends logs in batches with a default batch duration of 5 seconds.

### Added

- Added Close method to CloudWatchWriter which blocks until all the messages have been sent.
- Added SetBatchDuration method which easily allows the user to change the batch duration.

## [0.0.1] - 2020-08-12

### Added

- Basic synchronous implementation of NewWriter and NewWriterWithClient.

