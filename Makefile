.PHONY: start stop resume status logs-api logs-frontend down

start:
	kubectl apply -f k8s/

stop:
	kubectl scale deployment --all --replicas=0

resume:
	kubectl scale deployment postgres-deployment redis-deployment --replicas=1
	kubectl scale deployment api-deployment frontend-deployment --replicas=2

status:
	kubectl get pods,svc,pvc

logs-api:
	kubectl logs -l app=api --tail=20 -f

logs-frontend:
	kubectl logs -l app=frontend --tail=20 -f

down:
	kubectl delete -f k8s/