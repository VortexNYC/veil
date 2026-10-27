# RecoveryRequest

One initiator -> confirmer recovery ceremony row (VEIL-78). Owner callers also see human_email, current_epoch, and confirmer_wrapped_master (the caller's own share blob for the proof_hmac fast path); the initiator/target member sees the bare row.

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **str** |  | 
**org_id** | **str** |  | 
**human_id** | **str** | The member being recovered. | 
**initiated_by** | **str** | Who filed the request — the member (self) or an owner. | 
**new_pubkey** | **bytes** | Staged 32-byte X25519 public key, base64. | 
**reset_mfa** | **bool** | On completion, wipe the member&#39;s Kratos totp/webauthn and mail a re-enrollment link to their address. | 
**status** | **str** |  | 
**created_at** | **datetime** |  | 
**unlock_at** | **datetime** | Immediate for owner-confirmed paths; created_at+24h for sole-owner self-recovery. | 
**expires_at** | **datetime** |  | 
**resolved_at** | **datetime** |  | [optional] 
**confirmed_by** | **str** |  | [optional] 
**result_epoch** | **int** | The member&#39;s new wrap_pubkey_epoch after completion. | [optional] 
**human_email** | **str** | Owner view only — the member&#39;s resolved Kratos address. | [optional] 
**current_epoch** | **int** | Owner view only — the member&#39;s current wrap_pubkey_epoch. | 
**confirmer_wrapped_master** | **bytes** | Owner view only — the caller&#39;s own wrapped share (80B base64) for computing proof_hmac locally. | [optional] 

## Example

```python
from veil.models.recovery_request import RecoveryRequest

# TODO update the JSON string below
json = "{}"
# create an instance of RecoveryRequest from a JSON string
recovery_request_instance = RecoveryRequest.from_json(json)
# print the JSON string representation of the object
print(RecoveryRequest.to_json())

# convert the object into a dict
recovery_request_dict = recovery_request_instance.to_dict()
# create an instance of RecoveryRequest from a dict
recovery_request_from_dict = RecoveryRequest.from_dict(recovery_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


