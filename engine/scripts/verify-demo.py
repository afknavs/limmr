#!/usr/bin/env python3
"""Compatibility entry point for the full invented Circles HTTP/MCP proof."""
from demo_proof import main
if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, AssertionError, ValueError, KeyError) as error:
        raise SystemExit(str(error) or "Demo response assertion failed (private output withheld).") from None
