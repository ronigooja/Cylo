#!/usr/bin/env python3
"""研究mock：验证建议调度不变量；不验证上游额度、真实通话或工具能力。"""
import pathlib
import subprocess
import sys

repo = pathlib.Path(sys.argv[1])
sha = '6338ac478070aaa34bfe7a3c0469caf9e9174c02'
assert subprocess.check_output(['git', '-C', str(repo), 'rev-parse', 'HEAD'], text=True).strip() == sha
service = (repo / 'internal/voice/service.go').read_text()
html = (repo / 'static/voice.html').read_text()
pool = (repo / 'internal/accounts/pool.go').read_text()
assert '"client_tools":                  []any{}' in service
assert 'if status == 401 {' in service and 'StatusCode: 502,' in service
assert 'requireStickyAccount' in service and 'ORDER BY CASE WHEN last_used_at' in pool
assert "type: 'relay_message'" in html and "payload: { action: 'stop_speaking' }" in html
print('PASS source assertions: empty client_tools, 401/502 branches, LRU/sticky, relay/stop construction')

class PoolMock:
    def __init__(self):
        self.accounts = {1: {'enabled': True, 'until': 0}, 2: {'enabled': True, 'until': 0}}
        self.leases = {}
        self.quarantine = set()
        self.sessions = {}
        self.commands = set()
        self.clock = 0
        self.epoch = 0

    def acquire(self, session, preferred=None):
        if session in self.sessions:
            account, epoch, generation = self.sessions[session]
            if self.leases.get(account) == (session, epoch):
                return account, epoch, generation
            raise ValueError('stale session cannot overwrite lease')
        candidates = [preferred] if preferred is not None else sorted(self.accounts)
        for account in candidates:
            a = self.accounts[account]
            if a['enabled'] and a['until'] <= self.clock and account not in self.leases and account not in self.quarantine:
                self.epoch += 1
                result = (account, self.epoch, self.epoch)
                self.leases[account] = (session, self.epoch)
                self.sessions[session] = result
                return result
        raise ValueError('no eligible slot')

    def retire(self, session, epoch, exhausted=False, close_ack=False):
        account, actual, generation = self.sessions[session]
        if actual != epoch:
            raise ValueError('old epoch')
        if not close_ack:
            self.quarantine.add(account)
        del self.leases[account]
        del self.sessions[session]
        if exhausted:
            self.accounts[account]['until'] = self.clock + 60  # local retry cooldown, NOT upstream reset
        return {'old_generation': generation, 'upstream_conversation_id': None, 'reset_at': None}

    def commit(self, conversation, turn, revision):
        key = (conversation, turn, revision)
        new = key not in self.commands
        self.commands.add(key)
        return new

p = PoolMock()
a = p.acquire('s1')
assert a[0] == 1 and p.acquire('s1') == a
assert p.acquire('s2')[0] == 2
try:
    p.acquire('s3')
    raise AssertionError('overbooked')
except ValueError:
    pass
print('PASS mock: sticky reacquire; two slots; concurrent third selection rejected')
ctx = p.retire('s1', a[1], exhausted=True, close_ack=True)
assert ctx['upstream_conversation_id'] is None and ctx['reset_at'] is None
p.retire('s2', p.sessions['s2'][1], close_ack=True)
b = p.acquire('s1')
assert b[0] == 2 and b[2] != a[2]
print('PASS mock: exhausted account excluded; replacement gets new generation and clears upstream context')
try:
    p.retire('s1', a[1], close_ack=True)
    raise AssertionError('stale epoch accepted')
except ValueError:
    pass
assert p.commit('conversation', 'turn', 1) and not p.commit('conversation', 'turn', 1)
print('PASS mock: stale epoch rejected; repeated committed turn produces one command')
p.retire('s1', b[1], close_ack=False)
p.clock = 61
c = p.acquire('s4')
assert c[0] == 1 and 2 in p.quarantine
p.accounts[1]['enabled'] = False
p.retire('s4', c[1], close_ack=True)
try:
    p.acquire('s5')
    raise AssertionError('disabled/quarantine account reused')
except ValueError:
    pass
print('PASS mock: local cooldown retry eligibility; unconfirmed media closure quarantined; disabled excluded')
print('NOT TESTED: actual Go code execution, transactions/concurrency races, upstream quota/reset, WebRTC, final boundary, relay acceptance, tools, sideband, speech fidelity')
