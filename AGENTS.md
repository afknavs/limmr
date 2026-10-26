# Contributor safety rules
Use invented data and throwaway LIMMR_HOME directories for development.
Never send WhatsApp messages, reactions, typing indicators, read receipts or presence while testing.
Never access an existing installation's databases, credentials, services or settings.
Never commit secrets or real chat data. Use the fake WhatsApp implementation for tests.
Build offline with cached dependencies. Keep changes reversible and record verification limits.
Do not launch a bundled app during automated review; use the unbundled offscreen demo snapshot workflow.
See CONTRIBUTING.md for build commands. Publication and installation require separate owner authorization.
