# API Reference

## Packages
- [berth.skaphos.io/v1alpha1](#berthskaphosiov1alpha1)


## berth.skaphos.io/v1alpha1

Package v1alpha1 defines the Kubernetes custom resource types for the Berth
lease coordination API.

The primary resource is [BerthLease], which represents a distributed lease
that coordinates workload access across Kubernetes clusters. Each lease
tracks a holder identity, TTL-based expiration, heartbeat intervals, and
optional workload targeting for operator-driven suspend/resume actions.

Lease semantics are selected with the Semantics field. The CRD currently
accepts "at-most-once" and "at-least-once", but the central API server
enforces exclusive holder behavior today; "at-least-once" is reserved for a
future lease-window behavior.

Workload targeting:

A lease may optionally reference a target workload via [TargetRef]. When
specified, the Berth operator applies [LeaseAction] operations (such as
suspending or resuming) to the target workload in response to lease state
transitions.

Registration:

All types in this package are registered with the controller-runtime scheme
via [SchemeBuilder] and [AddToScheme]. The API group is "berth.skaphos.io".

	utilruntime.Must(v1alpha1.AddToScheme(scheme))


### Resource Types
- [BerthLease](#berthlease)
- [BerthLeaseList](#berthleaselist)



#### BerthLease



BerthLease is the schema for the BerthLease API.



_Appears in:_
- [BerthLeaseList](#berthleaselist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `berth.skaphos.io/v1alpha1` | | |
| `kind` _string_ | `BerthLease` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BerthLeaseSpec](#berthleasespec)_ |  |  |  |
| `status` _[BerthLeaseStatus](#berthleasestatus)_ |  |  |  |


#### BerthLeaseList



BerthLeaseList contains a list of BerthLease resources.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `berth.skaphos.io/v1alpha1` | | |
| `kind` _string_ | `BerthLeaseList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[BerthLease](#berthlease) array_ |  |  |  |


#### BerthLeaseSpec



BerthLeaseSpec declares the desired state for a Berth lease.



_Appears in:_
- [BerthLease](#berthlease)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `leaseName` _string_ | LeaseName is the unique identifier for this lease within its namespace. |  |  |
| `holderIdentity` _string_ | HolderIdentity identifies the entity requesting or holding the lease. |  |  |
| `ttlSeconds` _integer_ | TTLSeconds is the time-to-live for the lease in seconds. The lease<br />expires if not renewed within this duration. |  |  |
| `heartbeatIntervalSeconds` _integer_ | HeartbeatIntervalSeconds is the interval at which the holder must<br />renew the lease to prevent TTL expiration. |  |  |
| `semantics` _string_ | Semantics selects the lease-window behavior. "at-most-once" is the<br />implemented exclusive-holder mode. "at-least-once" is accepted by the<br />schema but currently behaves as exclusive-holder mode until the central<br />API server implements at-least-once lease windows. |  | Enum: [at-most-once at-least-once] <br /> |
| `target` _[TargetRef](#targetref)_ | Target is an optional reference to a workload the operator manages<br />in response to lease state transitions. |  |  |
| `acquireAction` _[LeaseAction](#leaseaction)_ | AcquireAction defines the action applied to the target workload when<br />the lease is acquired. |  |  |
| `releaseAction` _[LeaseAction](#leaseaction)_ | ReleaseAction defines the action applied to the target workload when<br />the lease is released or expires. |  |  |


#### BerthLeaseStatus



BerthLeaseStatus reports the observed state of a Berth lease.



_Appears in:_
- [BerthLease](#berthlease)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `observedGeneration` _integer_ | ObservedGeneration is the most recent .metadata.generation observed by<br />the operator. It is set on every status write so clients can tell<br />whether status reflects the current spec. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `leaseState` _string_ | LeaseState is the current state of the lease (e.g. "held", "released", "expired"). |  |  |
| `currentHolder` _string_ | CurrentHolder is the identity of the entity currently holding the lease. |  |  |
| `tenant` _string_ | Tenant is the resolved tenant identifier for the current holder. |  |  |
| `acquiredAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | AcquiredAt is the timestamp when the lease was last acquired. |  |  |
| `expiresAt` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | ExpiresAt is the timestamp when the lease will expire if not renewed. |  |  |
| `lastHeartbeat` _[Time](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#time-v1-meta)_ | LastHeartbeat is the timestamp of the most recent lease renewal. |  |  |
| `fencingToken` _integer_ | FencingToken is the monotonic fencing token returned by the central<br />API server on the most recent successful Acquire/Renew. It is used by<br />the reconciler on deletion to perform a best-effort Release. |  |  |
| `conditions` _[Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.36/#condition-v1-meta) array_ | Conditions represent the latest observations of the lease's state. |  |  |


#### LeaseAction



LeaseAction describes an action the operator may take on a target workload
when a lease state transition occurs. At most one of Suspend or Scale may
be set on a single action.



_Appears in:_
- [BerthLeaseSpec](#berthleasespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `suspend` _boolean_ | Suspend, when non-nil, sets the suspend field on the target workload.<br />Setting this to true pauses the workload; false resumes it. Applies to<br />workload kinds that expose a spec.suspend field, such as CronJob. |  |  |
| `scale` _[ScaleAction](#scaleaction)_ | Scale, when non-nil, sets the replica count on the target workload's<br />scale subresource. Applies to workload kinds that expose a scale<br />subresource, such as Deployment, StatefulSet, and ReplicaSet. |  |  |


#### ScaleAction



ScaleAction sets the replica count on the target workload's scale
subresource.



_Appears in:_
- [LeaseAction](#leaseaction)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _integer_ | Replicas is the desired replica count. Use 0 to scale a workload down<br />to zero (for example, on lease release). |  | Minimum: 0 <br /> |


#### TargetRef



TargetRef identifies a workload target managed by the operator in response
to lease state transitions. All three fields are required.



_Appears in:_
- [BerthLeaseSpec](#berthleasespec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | APIVersion is the API group and version of the target resource (e.g. "apps/v1"). |  |  |
| `kind` _string_ | Kind is the resource kind of the target (e.g. "Deployment"). |  |  |
| `name` _string_ | Name is the name of the target resource in the same namespace as the lease. |  |  |


