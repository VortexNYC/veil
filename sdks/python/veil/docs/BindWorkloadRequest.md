# BindWorkloadRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**issuer** | **str** | OIDC issuer URL | 
**subject** | **str** | expected token sub claim | 
**audience** | **str** | expected token aud claim | 

## Example

```python
from veil.models.bind_workload_request import BindWorkloadRequest

# TODO update the JSON string below
json = "{}"
# create an instance of BindWorkloadRequest from a JSON string
bind_workload_request_instance = BindWorkloadRequest.from_json(json)
# print the JSON string representation of the object
print(BindWorkloadRequest.to_json())

# convert the object into a dict
bind_workload_request_dict = bind_workload_request_instance.to_dict()
# create an instance of BindWorkloadRequest from a dict
bind_workload_request_from_dict = BindWorkloadRequest.from_dict(bind_workload_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


