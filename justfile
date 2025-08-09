default: 
    just --list

transactions:
    uv run dinero transactions

[working-directory: 'dashboards']
dashboards:
    uv run streamlit run balances.py

image:
    docker buildx build --platform linux/amd64 -t dinero .
