import os

from plate_reader.forwarder import Forwarder, HttpForwarder, NoopForwarder


def build_forwarder() -> Forwarder:
    url = os.environ.get('SITE_AGENT_URL')
    return HttpForwarder(url) if url else NoopForwarder()
