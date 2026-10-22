generate_proto_code_grpc:
	protoc -I=proto --go_out=backend --go_opt=module=main --go-grpc_out=backend --go-grpc_opt=module=main instance.proto
	protoc -I=proto --go_out=api-service --go_opt=module=main --go-grpc_out=api-service --go-grpc_opt=module=main instance.proto
