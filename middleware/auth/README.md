## Auth module
**ncloud-api** uses **JWT** as a method of authentication

### Algorithm
**HS512** (HMAC-SHA512)

### Access token
#### Payload
```json
{
    "username": "username",
    "exp": "expiration date"
}
```
#### Time
Access token is created for **20 minutes**

### Refresh token
#### Payload
```json
{
    "username": "username",
    "exp": "expiration date"
}
```
#### Time
Refresh token is created for **7 days**