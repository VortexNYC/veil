# CreateShareRequest


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** | Label for the link (metadata only). | [optional] 
**blob_b64** | **str** | Base64 of the client-sealed envelope (nacl secretbox). The origin never sees plaintext or the key. | 
**meta** | **Dict[str, object]** | Envelope params JSON (alg/version/passphrase marker). Opaque to the origin. | [optional] 
**ttl_seconds** | **int** | Link lifetime; default 24h, max 30d. | [optional] 

## Example

```python
from veil.models.create_share_request import CreateShareRequest

# TODO update the JSON string below
json = "{}"
# create an instance of CreateShareRequest from a JSON string
create_share_request_instance = CreateShareRequest.from_json(json)
# print the JSON string representation of the object
print(CreateShareRequest.to_json())

# convert the object into a dict
create_share_request_dict = create_share_request_instance.to_dict()
# create an instance of CreateShareRequest from a dict
create_share_request_from_dict = CreateShareRequest.from_dict(create_share_request_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


