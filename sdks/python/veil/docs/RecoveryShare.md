# RecoveryShare

The member's own org-key share state (VEIL-78). pubkey and wrapped_master are ciphertext material only — the 32-byte X25519 public key (base64) and the 80-byte box.SealAnonymous blob (base64). The private key and the plaintext org master never leave the client/store.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**enrolled** | **bool** | Whether the member has an org_key_shares row. | 
**pubkey** | **bytes** | Member&#39;s 32-byte X25519 public key, base64. | [optional] 
**wrapped_master** | **bytes** | 80-byte box.SealAnonymous ciphertext of the org master, base64. | [optional] 
**wrap_pubkey_epoch** | **int** | Share wrap epoch; bumps on every completed recovery. | 
**org_key_version** | **int** | Org master version the share wraps. | 
**created_at** | **datetime** |  | [optional] 
**updated_at** | **datetime** |  | [optional] 

## Example

```python
from veil.models.recovery_share import RecoveryShare

# TODO update the JSON string below
json = "{}"
# create an instance of RecoveryShare from a JSON string
recovery_share_instance = RecoveryShare.from_json(json)
# print the JSON string representation of the object
print(RecoveryShare.to_json())

# convert the object into a dict
recovery_share_dict = recovery_share_instance.to_dict()
# create an instance of RecoveryShare from a dict
recovery_share_from_dict = RecoveryShare.from_dict(recovery_share_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


