# DeviceRegisterRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**platform** | **str** | push platform | 
**token** | **str** | device push token | 
**label** | **str** | device label | [optional] 

## Example

```python
from veil.models.device_register_request import DeviceRegisterRequest

# TODO update the JSON string below
json = "{}"
# create an instance of DeviceRegisterRequest from a JSON string
device_register_request_instance = DeviceRegisterRequest.from_json(json)
# print the JSON string representation of the object
print(DeviceRegisterRequest.to_json())

# convert the object into a dict
device_register_request_dict = device_register_request_instance.to_dict()
# create an instance of DeviceRegisterRequest from a dict
device_register_request_from_dict = DeviceRegisterRequest.from_dict(device_register_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


