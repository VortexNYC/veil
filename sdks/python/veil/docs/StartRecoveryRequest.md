# StartRecoveryRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**human_id** | **str** | Omit for self-recovery; set for owner-initiated recovery of a member. | [optional] 
**new_pubkey** | **str** | Staged 32-byte X25519 public key, base64 or hex. Must not match any enrolled share pubkey. | 
**reset_mfa** | **bool** | Also wipe the member&#39;s Kratos second factors on completion. | [optional] 

## Example

```python
from veil.models.start_recovery_request import StartRecoveryRequest

# TODO update the JSON string below
json = "{}"
# create an instance of StartRecoveryRequest from a JSON string
start_recovery_request_instance = StartRecoveryRequest.from_json(json)
# print the JSON string representation of the object
print(StartRecoveryRequest.to_json())

# convert the object into a dict
start_recovery_request_dict = start_recovery_request_instance.to_dict()
# create an instance of StartRecoveryRequest from a dict
start_recovery_request_from_dict = StartRecoveryRequest.from_dict(start_recovery_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


