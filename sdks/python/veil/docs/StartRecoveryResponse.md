# StartRecoveryResponse


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**request** | [**RecoveryRequest**](RecoveryRequest.md) |  | 
**created** | **bool** | false means an open request already existed — the live row is returned and notifications were suppressed. | 

## Example

```python
from veil.models.start_recovery_response import StartRecoveryResponse

# TODO update the JSON string below
json = "{}"
# create an instance of StartRecoveryResponse from a JSON string
start_recovery_response_instance = StartRecoveryResponse.from_json(json)
# print the JSON string representation of the object
print(StartRecoveryResponse.to_json())

# convert the object into a dict
start_recovery_response_dict = start_recovery_response_instance.to_dict()
# create an instance of StartRecoveryResponse from a dict
start_recovery_response_from_dict = StartRecoveryResponse.from_dict(start_recovery_response_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


