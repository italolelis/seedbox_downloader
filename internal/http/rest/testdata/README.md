# Test Data

This directory contains test fixtures for the Transmission RPC handler tests.

## Files

### Bencode Test Data
Test bencode strings are generated inline in `transmission_test.go` using Go string literals.
This approach avoids external file dependencies and makes test cases self-documenting.

### Real .torrent Files
For integration tests with real .torrent files:
1. Obtain a valid .torrent file from your tracker (e.g., amigos-share)
2. Place it in this directory as `valid.torrent`
3. The file is gitignored to avoid committing copyrighted metadata

Note: Integration tests that require real .torrent files will skip if the fixture is not present.

## Bencode Format Reference

Valid .torrent structure (simplified):
- Root: dictionary (`d...e`)
- Required field: `info` (dictionary with torrent metadata)
- Common fields: `announce`, `created by`, `creation date`

Example minimal valid bencode:
```
d4:infod4:name4:teste
```
Decoded: `{"info": {"name": "test"}}`

Example invalid cases:
- Not bencode: `not bencode data`
- List instead of dict: `l4:teste` (["test"])
- Missing info field: `d4:name4:teste` ({"name": "test"})
