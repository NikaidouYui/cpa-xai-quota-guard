import json
from datetime import datetime, timezone, timedelta

def is_xai(provider, auth_type=''):
    p=(provider or '').lower().replace(' ','')
    a=(auth_type or '').lower().replace(' ','')
    return p in ('xai','x_ai','x-ai') or a in ('xai','x_ai','x-ai')

def excluded(body):
    lower=body.lower()
    keys=['unauthorized','invalid_api_key','invalid api key','insufficient_quota','insufficient quota','banned','suspended','payment_required','billing hard limit','monthly limit','monthly quota']
    return any(k in lower for k in keys)

def rate_signal(body, headers=None):
    headers=headers or {}
    lower=body.lower()
    code=typ=msg=''
    try:
        obj=json.loads(body)
        def walk(v):
            nonlocal code,typ,msg
            if isinstance(v, dict):
                if 'code' in v and not code: code=str(v['code'])
                if 'type' in v and not typ: typ=str(v['type'])
                if 'message' in v and not msg: msg=str(v['message'])
                for c in v.values(): walk(c)
            elif isinstance(v, list):
                for c in v: walk(c)
        walk(obj)
    except Exception:
        pass
    codes={'rate_limit_exceeded','rate_limit','too_many_requests'}
    types={'tokens','requests','rate_limit_error','rate_limit_exceeded','rate_limit','too_many_requests'}
    if code.replace('-','_').lower() in codes: return True
    if typ.replace('-','_').lower() in types: return True
    needles=['rate limit','rate_limit','too many requests','tokens per minute','requests per minute','rate_limit_exceeded']
    text=(msg or body).lower()
    if any(n in text for n in needles): return True
    if headers.get('x-ratelimit-remaining-requests')=='0' or headers.get('x-ratelimit-remaining-tokens')=='0':
        return True
    if any(k in headers for k in ['x-ratelimit-reset-requests','x-ratelimit-reset-tokens','x-ratelimit-reset']):
        return True
    return False

def parse_retry(body, headers, now):
    ra=headers.get('Retry-After') or headers.get('retry-after')
    if ra:
        try:
            return now+timedelta(seconds=float(ra))
        except Exception:
            pass
    for k in ['x-ratelimit-reset-requests','x-ratelimit-reset-tokens','x-ratelimit-reset']:
        v=headers.get(k)
        if v:
            try:
                n=float(v)
                if n>1e12: return datetime.fromtimestamp(n/1000, tz=timezone.utc)
                if n>1e9: return datetime.fromtimestamp(n, tz=timezone.utc)
                return now+timedelta(seconds=n)
            except Exception:
                pass
    try:
        obj=json.loads(body)
    except Exception:
        return None
    found=None
    def walk(v):
        nonlocal found
        if found is not None: return
        if isinstance(v, dict):
            for k,val in v.items():
                if k in ('retry_after','retryAfter','resets_in_seconds') and isinstance(val,(int,float,str)):
                    try:
                        found=now+timedelta(seconds=float(val)); return
                    except Exception:
                        pass
                walk(val)
        elif isinstance(v, list):
            for c in v: walk(c)
    walk(obj)
    return found

def match(provider, status, body, headers=None, failed=True):
    headers=headers or {}
    if not failed or not is_xai(provider) or status!=429: return False
    if excluded(body): return False
    if not rate_signal(body, headers): return False
    now=datetime.now(timezone.utc)
    rec=parse_retry(body, headers, now)
    return rec is not None and rec>now

cases=[
 ('ok body', True, match('xai',429,'{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached for requests per minute","type":"tokens","retry_after":120}}')),
 ('codex provider', False, match('codex',429,'{"error":{"type":"usage_limit_reached","resets_in_seconds":60}}')),
 ('auth', False, match('xai',401,'{"error":{"code":"invalid_api_key","message":"Incorrect API key"}}')),
 ('insufficient', False, match('xai',429,'{"error":{"message":"check your plan and billing details","code":"insufficient_quota"}}', {'Retry-After':'60'})),
 ('no reset', False, match('xai',429,'{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached for requests"}}')),
 ('codex body on xai', False, match('xai',429,'{"error":{"type":"usage_limit_reached","resets_in_seconds":30}}')),
 ('header reset', True, match('xai',429,'{"error":{"message":"Too Many Requests"}}', {'x-ratelimit-remaining-requests':'0','x-ratelimit-reset-requests':'90'})),
]
fail=0
for name,exp,got in cases:
    ok = got==exp
    print(('PASS' if ok else 'FAIL'), name, 'exp',exp,'got',got)
    if not ok: fail+=1
print('fails',fail)
raise SystemExit(fail)
