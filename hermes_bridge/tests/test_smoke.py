"""Smoke tests for the hermes_bridge package.

Proves the dev packaging works: the package is importable, its submodules
load without pulling in hermes-agent deps, and pytest discovers this file
via pyproject.toml's testpaths.
"""


def test_package_importable():
    import hermes_bridge  # noqa: F401


def test_callbacks_module_loads():
    # callbacks.py imports only stdlib + hermes_bridge submodules, so it
    # must load cleanly in a dev venv that has no hermes-agent installed.
    from hermes_bridge import callbacks

    assert hasattr(callbacks, "post_event")


def test_http_client_module_loads():
    from hermes_bridge import http_client

    assert hasattr(http_client, "unix_post")
