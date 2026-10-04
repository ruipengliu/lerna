package collaboration

import "github.com/ruipengliu/lerna/api"

// RemoteSessionLegacyCreateContract 固定保存增加 session_context 之前的受信合同。
// 只用于恢复原 journal Entry；新准入始终使用当前合同。
func RemoteSessionLegacyCreateContract() (api.MethodContract, error) {
	var contract api.MethodContract
	if err := api.Decode([]byte(remoteSessionLegacyCreateContract), &contract); err != nil {
		return contract, err
	}
	digest, err := api.Digest([]any{contract.InputSchema, contract.OutputSchema})
	if err != nil || contract.Name != "collaboration.create" || digest != "sha256:337ee8a9c427285b156d937f8782d1e836d12e58a6e7379b1e537efa2dba66e7" || contract.SchemaDigest != digest {
		return api.MethodContract{}, api.E("unsupported", "remote_session_legacy_contract_invalid")
	}
	return contract, nil
}

const remoteSessionLegacyCreateContract = `{
  "name": "collaboration.create",
  "owner": "orchestrator",
  "kind": "command",
  "cas": false,
  "allows_accepted": true,
  "input_schema": {
    "additionalProperties": false,
    "properties": {
      "allocation_ref": {
        "$ref": "#/$defs/ObjectRef"
      },
      "ancestor_task_refs": {
        "items": {
          "$ref": "#/$defs/ObjectRef"
        },
        "maxItems": 100,
        "minItems": 0,
        "type": "array"
      },
      "child_task_id": {
        "maxLength": 4096,
        "type": "string"
      },
      "create_command_id": {
        "maxLength": 4096,
        "type": "string"
      },
      "creation_key": {
        "maxLength": 4096,
        "type": "string"
      },
      "delegation_ref": {
        "$ref": "#/$defs/ObjectRef"
      },
      "foreign_references": {
        "items": {
          "additionalProperties": false,
          "properties": {
            "content_ref": {
              "$ref": "#/$defs/ContentRef"
            },
            "copy_id": {
              "maxLength": 4096,
              "type": "string"
            },
            "holder_ref": {
              "$ref": "#/$defs/ObjectRef"
            },
            "location": {
              "maxLength": 4096,
              "type": "string"
            },
            "purpose": {
              "maxLength": 4096,
              "type": "string"
            },
            "reference_intent_ref": {
              "$ref": "#/$defs/ObjectRef"
            },
            "register_command_id": {
              "maxLength": 4096,
              "type": "string"
            },
            "release_command_id": {
              "maxLength": 4096,
              "type": "string"
            },
            "retain_until": {
              "maxLength": 4096,
              "type": "string"
            }
          },
          "required": [
            "content_ref",
            "copy_id",
            "register_command_id",
            "release_command_id",
            "reference_intent_ref",
            "holder_ref",
            "purpose",
            "location",
            "retain_until"
          ],
          "type": "object"
        },
        "maxItems": 100,
        "minItems": 0,
        "type": "array"
      },
      "input": {
        "additionalProperties": false,
        "properties": {
          "agent_binding_ref": {
            "$ref": "#/$defs/ObjectRef"
          },
          "budget": {
            "items": {
              "$ref": "#/$defs/Amount"
            },
            "maxItems": 100,
            "minItems": 0,
            "type": "array"
          },
          "deadline": {
            "maxLength": 4096,
            "type": "string"
          },
          "delegation_id": {
            "maxLength": 4096,
            "type": "string"
          },
          "goal_ref": {
            "$ref": "#/$defs/ContentRef"
          },
          "input_refs": {
            "items": {
              "$ref": "#/$defs/ContentRef"
            },
            "maxItems": 100,
            "minItems": 0,
            "type": "array"
          },
          "internal": {
            "type": "boolean"
          },
          "parent_goal_revision": {
            "maximum": 9007199254740991,
            "minimum": 0,
            "type": "integer"
          },
          "parent_task_ref": {
            "$ref": "#/$defs/ObjectRef"
          },
          "permission_refs": {
            "items": {
              "$ref": "#/$defs/ObjectRef"
            },
            "maxItems": 100,
            "minItems": 0,
            "type": "array"
          },
          "policy_ref": {
            "$ref": "#/$defs/ComponentRef"
          },
          "receiver_id": {
            "maxLength": 4096,
            "type": "string"
          }
        },
        "required": [
          "delegation_id",
          "parent_task_ref",
          "parent_goal_revision",
          "goal_ref",
          "input_refs",
          "agent_binding_ref",
          "permission_refs",
          "budget",
          "deadline",
          "policy_ref",
          "receiver_id",
          "internal"
        ],
        "type": "object"
      },
      "original_command_ref": {
        "$ref": "#/$defs/ObjectRef"
      },
      "parent_admission": {
        "additionalProperties": false,
        "properties": {
          "action_scopes": {
            "items": {
              "additionalProperties": false,
              "properties": {
                "actions": {
                  "items": {
                    "maxLength": 4096,
                    "type": "string"
                  },
                  "maxItems": 100,
                  "minItems": 0,
                  "type": "array"
                },
                "binding_ref": {
                  "$ref": "#/$defs/ObjectRef"
                },
                "capability_ref": {
                  "$ref": "#/$defs/ComponentRef"
                },
                "location": {
                  "maxLength": 4096,
                  "type": "string"
                },
                "recipient": {
                  "maxLength": 4096,
                  "type": "string"
                },
                "resource_refs": {
                  "items": {
                    "$ref": "#/$defs/ComponentRef"
                  },
                  "maxItems": 100,
                  "minItems": 0,
                  "type": "array"
                },
                "resources": {
                  "items": {
                    "maxLength": 4096,
                    "type": "string"
                  },
                  "maxItems": 100,
                  "minItems": 0,
                  "type": "array"
                }
              },
              "required": [
                "capability_ref",
                "binding_ref",
                "resources",
                "resource_refs",
                "actions",
                "recipient",
                "location"
              ],
              "type": "object"
            },
            "maxItems": 100,
            "minItems": 0,
            "type": "array"
          },
          "capability_refs": {
            "items": {
              "$ref": "#/$defs/ComponentRef"
            },
            "maxItems": 100,
            "minItems": 0,
            "type": "array"
          },
          "controls": {
            "additionalProperties": false,
            "properties": {
              "max_action_duration_seconds": {
                "maximum": 9007199254740991,
                "minimum": 0,
                "type": "integer"
              },
              "max_actions_per_decision": {
                "maximum": 9007199254740991,
                "minimum": 0,
                "type": "integer"
              },
              "max_call_cost_bound": {
                "items": {
                  "$ref": "#/$defs/Amount"
                },
                "maxItems": 100,
                "minItems": 0,
                "type": "array"
              },
              "max_delegations_per_decision": {
                "maximum": 9007199254740991,
                "minimum": 0,
                "type": "integer"
              },
              "max_depth": {
                "maximum": 9007199254740991,
                "minimum": 0,
                "type": "integer"
              },
              "max_input_bytes": {
                "maximum": 9007199254740991,
                "minimum": 0,
                "type": "integer"
              },
              "max_output_tokens": {
                "maximum": 9007199254740991,
                "minimum": 0,
                "type": "integer"
              }
            },
            "required": [
              "max_input_bytes",
              "max_output_tokens",
              "max_actions_per_decision",
              "max_delegations_per_decision",
              "max_depth",
              "max_action_duration_seconds",
              "max_call_cost_bound"
            ],
            "type": "object"
          },
          "decision_id": {
            "maxLength": 4096,
            "type": "string"
          },
          "install_lock_ref": {
            "$ref": "#/$defs/ComponentRef"
          },
          "knowledge": {
            "additionalProperties": false,
            "properties": {
              "packet_ref": {
                "$ref": "#/$defs/ContentRef"
              },
              "selection_digest": {
                "maxLength": 4096,
                "type": "string"
              },
              "selection_id": {
                "maxLength": 4096,
                "type": "string"
              }
            },
            "required": [
              "selection_id",
              "selection_digest",
              "packet_ref"
            ],
            "type": "object"
          },
          "model_profile_ref": {
            "$ref": "#/$defs/ComponentRef"
          },
          "model_scope": {
            "additionalProperties": false,
            "properties": {
              "location": {
                "maxLength": 4096,
                "type": "string"
              },
              "model_profile_ref": {
                "$ref": "#/$defs/ComponentRef"
              },
              "receiver": {
                "maxLength": 4096,
                "type": "string"
              }
            },
            "required": [
              "model_profile_ref",
              "receiver",
              "location"
            ],
            "type": "object"
          },
          "snapshot_id": {
            "maxLength": 4096,
            "type": "string"
          },
          "snapshot_ref": {
            "$ref": "#/$defs/ContentRef"
          },
          "task_ref": {
            "$ref": "#/$defs/ObjectRef"
          },
          "use_digest": {
            "maxLength": 4096,
            "type": "string"
          },
          "use_intent_hash": {
            "maxLength": 4096,
            "type": "string"
          },
          "use_ref": {
            "$ref": "#/$defs/ObjectRef"
          },
          "valid_until": {
            "maxLength": 4096,
            "type": "string"
          }
        },
        "required": [
          "task_ref",
          "decision_id",
          "snapshot_id",
          "snapshot_ref",
          "model_profile_ref",
          "install_lock_ref",
          "capability_refs",
          "action_scopes",
          "controls",
          "valid_until"
        ],
        "type": "object"
      },
      "parent_sources": {
        "items": {
          "$ref": "#/$defs/SourceEvidence"
        },
        "maxItems": 100,
        "minItems": 0,
        "type": "array"
      },
      "profile_ref": {
        "$ref": "#/$defs/ComponentRef"
      },
      "source_database_id": {
        "maxLength": 4096,
        "type": "string"
      },
      "subject_ref": {
        "$ref": "#/$defs/ObjectRef"
      }
    },
    "required": [
      "creation_key",
      "create_command_id",
      "child_task_id",
      "profile_ref",
      "delegation_ref",
      "allocation_ref",
      "source_database_id",
      "subject_ref",
      "original_command_ref",
      "parent_sources",
      "input",
      "ancestor_task_refs",
      "foreign_references"
    ],
    "type": "object"
  },
  "output_schema": {
    "additionalProperties": false,
    "properties": {
      "child_task_ref": {
        "$ref": "#/$defs/ObjectRef"
      },
      "creation_key": {
        "maxLength": 4096,
        "type": "string"
      },
      "phase": {
        "maxLength": 4096,
        "type": "string"
      }
    },
    "required": [
      "creation_key",
      "phase"
    ],
    "type": "object"
  },
  "schema_digest": "sha256:337ee8a9c427285b156d937f8782d1e836d12e58a6e7379b1e537efa2dba66e7",
  "recovery": "query original command/object; preserve exact owner, payload, revision and deadlines"
}`
